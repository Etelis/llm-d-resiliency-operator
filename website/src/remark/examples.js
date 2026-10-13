const MARKER = /^example ([\w-]+)$/;

function onlyImage(node) {
  return (
    node &&
    node.type === 'paragraph' &&
    node.children.every((child) => child.type === 'image' || (child.type === 'text' && !child.value.trim())) &&
    node.children.some((child) => child.type === 'image')
  );
}

export default function examples() {
  return (tree) => {
    const visit = (parent) => {
      if (!parent.children) return;
      for (let i = 0; i < parent.children.length; i++) {
        const node = parent.children[i];
        const match = node.type === 'definition' && node.label.match(MARKER);
        if (!match) {
          visit(node);
          continue;
        }
        parent.children[i] = {
          type: 'mdxJsxFlowElement',
          name: 'Example',
          attributes: [{type: 'mdxJsxAttribute', name: 'name', value: match[1]}],
          children: [],
        };
        while (onlyImage(parent.children[i - 1])) {
          parent.children.splice(i - 1, 1);
          i--;
        }
      }
    };
    visit(tree);
  };
}
