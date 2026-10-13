import path from 'node:path';
import {fileURLToPath} from 'node:url';

const repo = 'https://github.com/llm-d-incubation/llm-d-resiliency-operator/tree/main/';
const root = fileURLToPath(new URL('../../../', import.meta.url));
const docs = path.join(root, 'docs');

export default function repoLinks() {
  return (tree, file) => {
    const visit = (node) => {
      if (node.type === 'link' && node.url && !/^([a-z]+:|#|\/)/i.test(node.url)) {
        const [target, anchor] = node.url.split('#');
        const resolved = path.resolve(path.dirname(file.path), target);
        if (!resolved.startsWith(docs + path.sep)) {
          node.url = repo + path.relative(root, resolved) + (anchor ? `#${anchor}` : '');
        }
      }
      (node.children || []).forEach(visit);
    };
    visit(tree);
  };
}
