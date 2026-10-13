import React from 'react';
import Link from '@docusaurus/Link';
import Scenario from '@site/src/components/Scenario';
import styles from './styles.module.css';

const examples = require.context('../../../../docs/scenarios', false, /\.json$/);

export default function Example({name}) {
  const data = examples(`./${name}.json`);
  return (
    <div className={styles.example}>
      <div className={styles.head}>
        <span className={styles.label}>Example</span>
        {data.page ? <Link to={`/docs/${data.page}`}>{data.title}</Link> : <span>{data.title}</span>}
      </div>
      <Scenario data={data} />
    </div>
  );
}
