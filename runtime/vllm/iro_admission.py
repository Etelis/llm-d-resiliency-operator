# Copyright 2026 The llm-d Authors.
# SPDX-License-Identifier: Apache-2.0
"""Enforce IRO admission without changing vLLM health."""

import hmac
import os

import httpx
from starlette.requests import Request
from starlette.responses import JSONResponse

# Include inference aliases outside the OpenAI-compatible API.
INFERENCE_ALIASES = frozenset(
    {
        "/invocations",
        "/generative_scoring",
        "/inference/v1/generate",
        "/cohere/v2/chat",
        "/score",
        "/rerank",
        "/pooling",
        "/classify",
        "/v2/embed",
        "/v2/rerank",
    }
)


class AdmissionMiddleware:
    def __init__(self, app):
        self.app = app
        self.client = httpx.AsyncClient(timeout=1.0)
        self.controller = os.environ["IRO_CONTROLLER_URL"]
        self.uid = os.environ["POD_UID"]
        self.token = os.environ["IRO_VERIFICATION_TOKEN"]
        if not self.token:
            raise ValueError("IRO_VERIFICATION_TOKEN must be non-empty")

    async def state(self, scope):
        try:
            client = scope["app"].state.engine_client
            status = await client.get_status()
            engines = status["engines"]
            if len(engines) != 1:
                return -1, False
            rank = engines[0]["id"]
            if type(rank) is not int or rank < 0:
                return -1, False
            reply = await self.client.get(
                self.controller + "/admission",
                params={"rank": rank, "pod_uid": self.uid},
            )
            reply.raise_for_status()
            return rank, reply.json()["admitted"] is True
        except Exception:
            return -1, False

    async def __call__(self, scope, receive, send):
        if scope["type"] == "lifespan":

            async def lifespan_send(message):
                if message["type"] == "lifespan.shutdown.complete":
                    await self.client.aclose()
                await send(message)

            return await self.app(scope, receive, lifespan_send)
        if scope["type"] != "http":
            return await self.app(scope, receive, send)
        path = scope["path"].rstrip("/")
        metrics = path == "/metrics"
        inference = path in INFERENCE_ALIASES or (
            path.startswith("/v1/") and not (path.startswith("/v1/fault_tolerance/") or path == "/v1/models")
        )
        if not metrics and not inference:
            return await self.app(scope, receive, send)
        rank, admitted = await self.state(scope)
        # The shared token authorizes only the adapter's completion probe.
        verification = (
            scope["method"] == "POST"
            and path == "/v1/completions"
            and bool(self.token)
            and hmac.compare_digest(Request(scope).headers.get("X-IRO-Verification", ""), self.token)
        )
        if inference and not admitted and not verification:
            return await JSONResponse({"error": "rank is not admitted by IRO"}, status_code=503)(scope, receive, send)
        if not metrics:

            async def mark(message):
                if message["type"] == "http.response.start":
                    message["headers"] = list(message["headers"]) + [(b"x-iro-rank", str(rank).encode())]
                await send(message)

            return await self.app(scope, receive, mark)

        # Preserve native metrics and append the independent admission gate.
        scope = dict(scope)
        scope["headers"] = [(k, v) for k, v in scope["headers"] if k.lower() != b"accept-encoding"]
        start = None
        chunks = []

        async def capture(message):
            nonlocal start
            if message["type"] == "http.response.start":
                start = dict(message)
            elif message["type"] == "http.response.body":
                chunks.append(message.get("body", b""))

        await self.app(scope, receive, capture)
        if start is None:
            return None
        body = (
            b"".join(chunks)
            + (
                "\n# HELP iro_rank_admitted Whether IRO admits this endpoint.\n"
                "# TYPE iro_rank_admitted gauge\n"
                f"iro_rank_admitted {int(admitted)}\n"
            ).encode()
        )
        start["headers"] = [(k, v) for k, v in start["headers"] if k.lower() != b"content-length"] + [
            (b"content-length", str(len(body)).encode())
        ]
        await send(start)
        await send({"type": "http.response.body", "body": body})
        return None
