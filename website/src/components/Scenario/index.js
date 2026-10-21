import React, {useEffect, useMemo, useState} from 'react';
import styles from './styles.module.css';

const LABELS = {
  serving: 'serving',
  paused: 'paused',
  dead: 'dead',
  excluded: 'excluded',
  starting: 'starting',
  joining: 'joining',
  unknown: 'unread',
};

const TARGETS = {
  workload: 'Workload',
  driver: 'Engine driver',
  policy: 'Engine policy',
  journal: 'Journal',
  telemetry: 'Telemetry',
};

const ENGINES = [
  {id: 'vllm', label: 'vLLM'},
  {id: 'sglang', label: 'SGLang', soon: 'No SGLang engine yet'},
];

function frames(data) {
  let pods = (data.pods || []).map((pod) => ({...pod, members: {...pod.members}}));
  return data.steps.map((step) => {
    pods = pods.map((pod) => {
      const change = (step.pods || {})[pod.name];
      if (!change) return pod;
      const {uid, gone, ...members} = change;
      return {
        ...pod,
        uid: uid || pod.uid,
        gone: gone === undefined ? pod.gone && !uid : gone,
        members: {...pod.members, ...members},
      };
    });
    return {...step, pods};
  });
}

function podState(pod) {
  if (pod.gone) return 'gone';
  const states = Object.values(pod.members);
  for (const s of ['dead', 'excluded', 'unknown', 'starting', 'joining', 'paused']) {
    if (states.includes(s)) return s;
  }
  return 'serving';
}

function Pod({pod}) {
  const state = podState(pod);
  return (
    <div className={`${styles.pod} ${styles[`pod_${state}`]}`}>
      <div className={styles.podName}>{pod.name}</div>
      <div className={styles.podUid}>{pod.gone ? 'deleted' : `UID ${pod.uid}`}</div>
      <div className={styles.members}>
        {Object.entries(pod.members).map(([name, s]) => (
          <span key={name} className={`${styles.member} ${styles[`m_${pod.gone ? 'gone' : s}`]}`} title={LABELS[s]}>
            {name}
          </span>
        ))}
      </div>
      <div className={`${styles.badge} ${styles[`b_${state}`]}`}>
        {pod.gone ? 'deleted' : LABELS[state]}
      </div>
    </div>
  );
}

function Request({request}) {
  return (
    <div className={styles.request}>
      <code className={styles.callName}>{request.call}</code>
      {request.in && (
        <div className={styles.io}>
          <span className={styles.ioTag}>in</span>
          <span>{request.in}</span>
        </div>
      )}
      {request.out && (
        <div className={styles.io}>
          <span className={`${styles.ioTag} ${styles.out}`}>out</span>
          <span>{request.out}</span>
        </div>
      )}
    </div>
  );
}

function Call({call, open, onToggle}) {
  const expandable = call.requests && call.requests.length > 0;
  return (
    <div>
      <div
        className={`${styles.call} ${expandable ? styles.callExpandable : ''}`}
        onClick={expandable ? onToggle : undefined}>
        <div className={styles.callHead}>
          {expandable && <span className={`${styles.chevron} ${open ? styles.chevronOpen : ''}`}>&#9654;</span>}
          <span className={`${styles.target} ${styles[`t_${call.to}`]}`}>{TARGETS[call.to] || call.to}</span>
          <code className={styles.callName}>{call.call}</code>
        </div>
        {call.in && (
          <div className={styles.io}>
            <span className={styles.ioTag}>in</span>
            <span>{call.in}</span>
          </div>
        )}
        {call.out && (
          <div className={styles.io}>
            <span className={`${styles.ioTag} ${styles.out}`}>out</span>
            <span>{call.out}</span>
          </div>
        )}
      </div>
      {expandable && open && (
        <div className={styles.requests}>
          {call.requests.map((request, i) => (
            <Request key={i} request={request} />
          ))}
        </div>
      )}
    </div>
  );
}

export default function Scenario({data}) {
  const tabbed = Boolean(data.engines);
  const [engine, setEngine] = useState(tabbed ? ENGINES[0].id : null);
  const engineLabel = (ENGINES.find((e) => e.id === engine) || {}).label;
  const body = tabbed ? data.engines[engine] : data;
  const steps = useMemo(() => frames(body), [body]);
  const [index, setIndex] = useState(0);
  const [expanded, setExpanded] = useState(() => new Set());
  const step = steps[index];
  const go = (i) => setIndex(Math.max(0, Math.min(steps.length - 1, i)));
  const toggle = (key) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  const select = (id) => {
    setEngine(id);
    setIndex(0);
    setExpanded(new Set());
  };

  useEffect(() => {
    const match = window.location.hash.match(/^#step-(\d+)$/);
    if (match) go(Number(match[1]) - 1);
  }, []);

  const onKey = (event) => {
    if (event.key === 'ArrowRight') go(index + 1);
    if (event.key === 'ArrowLeft') go(index - 1);
  };

  return (
    <div className={styles.scenario} tabIndex={0} onKeyDown={onKey}>
      {tabbed && (
        <div className={styles.engines}>
          {ENGINES.map((e) => (
            <button
              key={e.id}
              type="button"
              className={`${styles.engineTab} ${engine === e.id ? styles.engineTabActive : ''}`}
              onClick={() => select(e.id)}
              disabled={Boolean(e.soon)}
              title={e.soon || e.label}>
              {e.label}
              {e.soon && <span className={styles.soon}>soon</span>}
            </button>
          ))}
        </div>
      )}

      <div className={styles.timeline}>
        {steps.map((s, i) => (
          <button
            key={i}
            type="button"
            className={`${styles.dot} ${i === index ? styles.dotActive : ''} ${styles[`phase_${s.phase}`]}`}
            onClick={() => go(i)}
            title={s.title}>
            {s.t || `t${i}`}
          </button>
        ))}
      </div>

      <div className={styles.header}>
        <div>
          <div className={styles.stepTitle}>{step.title}</div>
          {step.note && <div className={styles.note}>{step.note}</div>}
        </div>
        {step.phase && <span className={`${styles.phase} ${styles[`phase_${step.phase}`]}`}>{step.phase}</span>}
      </div>

      {step.pods.length > 0 && (
        <div className={styles.pods}>
          {step.pods.map((pod) => (
            <Pod key={pod.name} pod={pod} />
          ))}
        </div>
      )}

      {step.internal && (
        <div className={styles.internal}>
          <span className={`${styles.target} ${styles.t_engine}`}>{engineLabel}</span>
          <span>{step.internal}</span>
        </div>
      )}

      {step.calls && step.calls.length > 0 && (
        <div className={styles.calls}>
          <div className={styles.callsTitle}>{body.callsTitle || data.callsTitle || 'What the controller calls'}</div>
          {step.calls.map((call, i) => (
            <Call key={i} call={call} open={expanded.has(`${index}-${i}`)} onToggle={() => toggle(`${index}-${i}`)} />
          ))}
        </div>
      )}

      {step.journal && (
        <div className={styles.journal}>
          <span className={`${styles.target} ${styles.t_journal}`}>Journal</span>
          <code>{step.journal}</code>
        </div>
      )}

      <div className={styles.nav}>
        <button type="button" className="button button--secondary button--sm" onClick={() => go(index - 1)} disabled={index === 0}>
          ◀ prev
        </button>
        <span className={styles.counter}>
          step {index + 1} of {steps.length}
        </span>
        <button
          type="button"
          className="button button--primary button--sm"
          onClick={() => go(index + 1)}
          disabled={index === steps.length - 1}>
          next ▶
        </button>
      </div>
    </div>
  );
}
