import React, {useEffect, useId, useMemo, useState} from 'react';
import styles from './styles.module.css';
import {frames} from './frames.mjs';

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
  policy: 'Engine policy (pure)',
  journal: 'Journal',
  telemetry: 'Telemetry',
};

const ENGINES = [
  {id: 'vllm', label: 'vLLM'},
  {id: 'sglang', label: 'SGLang', disabledReason: 'No SGLang engine yet'},
];

function memberState(member) {
  return typeof member === 'string' ? member : member.state;
}

function podState(pod) {
  if (pod.gone) return 'gone';
  const states = Object.values(pod.members).map(memberState);
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
        {Object.entries(pod.members).map(([name, member]) => {
          const s = memberState(member);
          if (typeof member === 'string') {
            return <span key={name} className={`${styles.member} ${styles[`m_${pod.gone ? 'gone' : s}`]}`} title={LABELS[s]}>{name}</span>;
          }
          return (
            <div key={name} className={styles.memberDetail}>
              <span className={`${styles.member} ${styles[`m_${pod.gone ? 'gone' : s}`]}`}>{name}</span>
              <dl className={styles.facts}>
                <dt>Engine status</dt><dd>{pod.gone ? 'no Pod' : member.status || 'not read'}</dd>
                <dt>/health</dt><dd>{member.health ?? 'not called'}</dd>
                {member.ft_state && <><dt>ft_state</dt><dd>{member.ft_state}</dd></>}
                {member.last_ft_request_id && <><dt>Request ID</dt><dd>{member.last_ft_request_id}</dd></>}
                <dt>IRO observation</dt><dd>{({serving: 'Serving', dead: 'Terminal', paused: 'Not serving, nonterminal', unknown: 'Unknown', starting: 'Not observed'})[s] || LABELS[s]}</dd>
                <dt>Membership</dt><dd>{member.membership}</dd>
              </dl>
            </div>
          );
        })}
      </div>
      {Object.values(pod.members).every((member) => typeof member === 'string') && (
        <div className={`${styles.badge} ${styles[`b_${state}`]}`}>{pod.gone ? 'deleted' : LABELS[state]}</div>
      )}
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

function Call({call}) {
  const expandable = call.requests && call.requests.length > 0;
  const [expanded, setExpanded] = useState(false);
  const requestsId = useId();
  const heading = (
    <>
      <span className={`${styles.target} ${styles[`t_${call.to}`]}`}>{TARGETS[call.to] || call.to}</span>
      <code className={styles.callName}>{call.call}</code>
    </>
  );
  return (
    <div>
      <div className={styles.call}>
        {expandable ? (
          <button
            type="button"
            className={`${styles.callHead} ${styles.callHeadButton}`}
            aria-expanded={expanded}
            aria-controls={requestsId}
            onClick={() => setExpanded((value) => !value)}>
            <span className={`${styles.chevron} ${expanded ? styles.chevronOpen : ''}`} aria-hidden="true">&#9654;</span>
            {heading}
          </button>
        ) : <div className={styles.callHead}>{heading}</div>}
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
      {expandable && (
        <div id={requestsId} className={styles.requests} hidden={!expanded}>
          <div className={styles.requestsTitle}>Driver HTTP requests</div>
          {call.call.startsWith('Observe') && <div className={styles.requestNote}>Ranks are read concurrently. Each rank's status is read before its health; a failed status read skips health. Each row sends one request to each listed target. Relevant response fields are shown.</div>}
          {call.requests.map((request, i) => <Request key={i} request={request} />)}
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
  const step = steps[index];
  const go = (i) => setIndex(Math.max(0, Math.min(steps.length - 1, i)));
  const select = (id) => {
    setEngine(id);
    setIndex(0);
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
              disabled={Boolean(e.disabledReason)}
              title={e.disabledReason || e.label}>
              {e.label}
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
        {step.phase && <span className={`${styles.phase} ${styles[`phase_${step.phase}`]}`}>Controller: {step.phase}</span>}
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
          <div className={styles.callsTitle}>{body.callsTitle || data.callsTitle || 'Selected controller calls, in order'}</div>
          {step.calls.map((call, i) => (
            <Call key={`${engine}-${index}-${i}`} call={call} />
          ))}
        </div>
      )}

      {step.decision && <div className={styles.internal}><span className={`${styles.target} ${styles.t_controller}`}>Controller decision</span><span>{step.decision}</span></div>}

      {step.journal && (
        <div className={styles.journal}>
          <span className={`${styles.target} ${styles.t_journal}`}>Saved journal</span>
          <code>{step.journal}</code>
        </div>
      )}

      <div className={styles.nav}>
        <button type="button" className="button button--secondary button--sm" onClick={() => go(index - 1)} disabled={index === 0}>
          prev
        </button>
        <span className={styles.counter}>
          step {index + 1} of {steps.length}
        </span>
        <button
          type="button"
          className="button button--primary button--sm"
          onClick={() => go(index + 1)}
          disabled={index === steps.length - 1}>
          next
        </button>
      </div>
    </div>
  );
}
