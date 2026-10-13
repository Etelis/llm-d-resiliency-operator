import React from 'react';
import Link from '@docusaurus/Link';
import Scenario from '@site/src/components/Scenario';
import styles from './styles.module.css';

const examples = require.context('../../../../docs/examples', false, /\.json$/);

export default function Example({name}) {
  const data = examples(`./${name}.json`);
  return (
    <div className={styles.example}>
      <div className={styles.head}>
        <span className={styles.label}>Example</span>
        <Link to={`/docs/examples/${name}`}>{data.title}</Link>
      </div>
      <Scenario data={data} />
    </div>
  );
}
