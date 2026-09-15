# Copyright 2026 The llm-d Authors.
# SPDX-License-Identifier: Apache-2.0
"""Admission must overrule a still-healthy excluded rank and survive control loss."""

from unittest.mock import AsyncMock, Mock

import httpx
import pytest
from fastapi import FastAPI
from fastapi.responses import PlainTextResponse
from fastapi.testclient import TestClient
from iro_admission import AdmissionMiddleware


@pytest.fixture
def excluded_endpoint(monkeypatch):
    monkeypatch.setenv("IRO_CONTROLLER_URL", "http://controller")
    monkeypatch.setenv("POD_UID", "original-pod")
    monkeypatch.setenv("IRO_VERIFICATION_TOKEN", "verification-token")
    app = FastAPI()
    app.state.engine_client = Mock(get_status=AsyncMock(return_value={"engines": [{"id": 3, "status": "healthy"}]}))

    @app.api_route("/{path:path}", methods=["GET", "POST"])
    async def endpoint(path: str):
        return {"path": path}

    gate = AdmissionMiddleware(app)
    gate.client = httpx.AsyncClient(
        transport=httpx.MockTransport(lambda request: httpx.Response(200, json={"admitted": False}))
    )

    async def wrapped(scope, receive, send):
        scope["app"] = app
        await gate(scope, receive, send)

    with TestClient(wrapped) as client:
        yield client


@pytest.mark.parametrize(
    "path",
    [
        "/invocations",
        "/invocations/",
        "/generative_scoring",
        "/inference/v1/generate",
        "/cohere/v2/chat",
        "/v1/chat/completions",
        "/v1/chat/completions/batch",
        "/v1/responses",
        "/v1/messages",
        "/score",
        "/rerank",
        "/pooling",
        "/classify",
        "/v2/embed",
        "/v2/rerank",
    ],
)
def test_excluded_endpoint_rejects_inference_aliases_even_with_verification_header(excluded_endpoint, path):
    for headers in ({}, {"X-IRO-Verification": "verification-token"}):
        reply = excluded_endpoint.post(path, json={}, headers=headers)
        assert reply.status_code == 503
        assert reply.json() == {"error": "rank is not admitted by IRO"}


@pytest.mark.parametrize(
    "method,path",
    [
        ("GET", "/health"),
        ("GET", "/ping"),
        ("POST", "/ping"),
        ("GET", "/v1/models"),
        ("GET", "/v1/fault_tolerance/status"),
        ("POST", "/v1/fault_tolerance/apply"),
        ("GET", "/fault_tolerance/status"),
        ("POST", "/fault_tolerance/apply"),
        ("POST", "/tokenize"),
        ("POST", "/collective_rpc"),
    ],
)
def test_health_metadata_and_control_remain_available(excluded_endpoint, method, path):
    reply = excluded_endpoint.request(method, path)
    assert reply.status_code == 200
    assert "x-iro-rank" not in reply.headers


def test_verification_bypass_is_limited_to_the_controller_completion_probe(excluded_endpoint):
    path = "/v1/completions"
    assert excluded_endpoint.post(path, json={}).status_code == 503
    assert excluded_endpoint.post(path, json={}, headers={"X-IRO-Verification": "incorrect-token"}).status_code == 503
    headers = {"X-IRO-Verification": "verification-token"}
    assert excluded_endpoint.post(path, json={}, headers=headers).status_code == 200
    assert excluded_endpoint.get(path, headers=headers).status_code == 503
    assert excluded_endpoint.post("/v1/unrelated", json={}, headers=headers).status_code == 503


@pytest.mark.parametrize(
    "admitted,unavailable", [(False, False), (False, True), (True, False), ("true", False), (1, False)]
)
def test_gate_combines_native_health_with_authoritative_admission(monkeypatch, admitted, unavailable):
    monkeypatch.setenv("IRO_CONTROLLER_URL", "http://controller")
    monkeypatch.setenv("POD_UID", "original-pod")
    monkeypatch.setenv("IRO_VERIFICATION_TOKEN", "verification-token")
    app = FastAPI()
    app.state.engine_client = Mock(get_status=AsyncMock(return_value={"engines": [{"id": 3, "status": "healthy"}]}))

    @app.post("/v1/completions")
    async def complete():
        return {"choices": [{"text": "two"}]}

    @app.get("/metrics")
    async def metrics():
        return PlainTextResponse('vllm:engine_healthy{engine="3"} 1\n')

    gate = AdmissionMiddleware(app)

    def controller(request):
        assert request.url.params["rank"] == "3"
        assert request.url.params["pod_uid"] == "original-pod"
        if unavailable:
            raise httpx.ConnectError("controller down", request=request)
        return httpx.Response(200, json={"admitted": admitted})

    gate.client = httpx.AsyncClient(transport=httpx.MockTransport(controller))

    async def wrapped(scope, receive, send):
        scope["app"] = app
        await gate(scope, receive, send)

    with TestClient(wrapped) as client:
        allowed = admitted is True
        reply = client.post("/v1/completions", json={})
        assert reply.status_code == (200 if allowed else 503)
        verified = client.post("/v1/completions", json={}, headers={"X-IRO-Verification": "verification-token"})
        assert verified.status_code == 200
        scrape = client.get("/metrics")
        assert 'vllm:engine_healthy{engine="3"} 1' in scrape.text
        assert f"iro_rank_admitted {int(allowed)}" in scrape.text
        assert int(scrape.headers["content-length"]) == len(scrape.content)
    assert gate.client.is_closed
