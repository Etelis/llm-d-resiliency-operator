import assert from 'node:assert/strict';
import {readFileSync, readdirSync} from 'node:fs';
import test from 'node:test';
import {frames} from './frames.mjs';

const scenario = (name) => JSON.parse(readFileSync(new URL(`../../../../docs/scenarios/${name}.json`, import.meta.url))).engines.vllm;
const state = (member) => typeof member === 'string' ? member : member.state;

test('recovered survivors are displayed as serving', () => {
  const step = frames(scenario('member-dies')).find((s) => s.title === 'The group serves with 3 ranks');
  for (const pod of step.pods.slice(0, 3)) assert.equal(state(Object.values(pod.members)[0]), 'serving');
});

test('an unreadable admitted rank makes the vLLM policy wait', () => {
  const step = scenario('member-unreadable').steps[1];
  assert.match(step.calls.find((c) => c.to === 'policy').out, /^Wait/);
});

test('a failed status read skips the health request', () => {
  const step = scenario('controller-blind').steps[1];
  const requests = step.calls.find((c) => c.call.startsWith('Observe')).requests;
  assert.equal(requests.filter((r) => r.call === 'GET /health').length, 0);
});

test('membership changes preserve terminal evidence and earlier frames', () => {
  const data = scenario('member-dies');
  const original = structuredClone(data);
  const steps = frames(data);
  const failed = steps[1].pods[3].members.dp3;
  const excluded = steps[3].pods[3].members.dp3;
  const kept = steps[4].pods[3].members.dp3;
  assert.equal(failed.membership, 'admitted');
  assert.equal(excluded.membership, 'excluded');
  assert.equal(kept.membership, 'rejected');
  for (const member of [failed, excluded, kept]) {
    assert.equal(member.state, 'dead');
    assert.equal(member.status, 'healthy');
    assert.equal(member.health, 503);
  }
  assert.deepEqual(data, original);
});

test('a serving replacement resets the group before planning', () => {
  const step = frames(scenario('pod-lost')).find((s) => s.title === 'A replacement serves before admission');
  assert.equal(step.phase, 'starting');
  assert.equal(step.pods[3].uid, 'd2');
  assert.equal(step.pods[3].members.dp3.state, 'serving');
  assert.ok(step.calls.some((c) => c.call.startsWith('ResetAll')));
  assert.ok(!step.calls.some((c) => c.call.startsWith('Plan')));
});

test('the hang and transient examples require different plans', () => {
  const hang = frames(scenario('member-hangs'))[1];
  assert.equal(hang.pods[2].members.dp2.status, 'healthy');
  assert.match(hang.calls.find((c) => c.to === 'policy').out, /^Wait/);
  const transient = frames(scenario('transient-fault'))[1];
  assert.ok(transient.pods.every((p) => Object.values(p.members)[0].status === 'unhealthy'));
  assert.match(transient.calls.find((c) => c.to === 'policy').out, /^Change/);
});

test('legacy member strings and replacement UIDs still work', () => {
  const data = {
    pods: [{name: 'pod', uid: 'old', members: {m0: 'serving', m1: 'paused'}}],
    steps: [{pods: {pod: {gone: true}}}, {pods: {pod: {uid: 'new', m0: 'starting', m1: 'starting'}}}],
  };
  const steps = frames(data);
  assert.equal(steps[0].pods[0].gone, true);
  assert.equal(steps[1].pods[0].gone, false);
  assert.equal(steps[1].pods[0].uid, 'new');
  assert.deepEqual(steps[0].pods[0].members, {m0: 'serving', m1: 'paused'});
});

for (const file of readdirSync(new URL('../../../../docs/scenarios/', import.meta.url))) {
  if (!file.endsWith('.json')) continue;
  const data = JSON.parse(readFileSync(new URL(`../../../../docs/scenarios/${file}`, import.meta.url)));
  if (!data.engines) continue;
  test(`${file}: calls respect driver boundaries and journal barriers`, () => {
    for (const step of frames(data.engines.vllm)) {
      for (const [index, call] of (step.calls || []).entries()) {
        if (call.requests) assert.equal(call.to, 'driver');
        if (/^(Apply|ResetAll)/.test(call.call)) {
          assert.equal(step.calls[index - 1]?.call, 'Save(record)', step.title);
        }
        if (call.call.startsWith('Observe')) {
          for (const pod of step.pods) {
            for (const [rank, member] of Object.entries(pod.members)) {
              const requests = (call.requests || []).filter((r) => r.in.includes(`${rank} on `));
              if (pod.gone || member.state === 'starting') {
                assert.equal(requests.length, 0, `${step.title}: ${rank}`);
                continue;
              }
              const status = requests.find((r) => r.call.endsWith('/status'));
              const health = requests.find((r) => r.call === 'GET /health');
              assert.ok(status, `${step.title}: missing status read for ${rank}`);
              if (member.status === 'unread') {
                assert.match(status.out, /timeout/);
                assert.equal(health, undefined);
              } else {
                assert.ok(status.out.includes(`"status":"${member.status}"`));
                assert.equal(health?.out, String(member.health));
              }
            }
          }
        }
        if (call.call.startsWith('Verify')) {
          const payload = JSON.parse(call.requests[0].in.split('; ')[1]);
          assert.deepEqual(payload, {prompt: 'One plus one equals', max_tokens: 8, temperature: 0, ignore_eos: true});
        }
      }
    }
  });
}
