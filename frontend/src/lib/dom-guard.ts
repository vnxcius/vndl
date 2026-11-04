// React assumes it's the only thing moving the nodes it created. Page
// translators, extensions and scripts injected by in-app browsers break that by
// re-parenting them, and the resulting NotFoundError takes down the whole page.
// See https://github.com/facebook/react/issues/11538.
const { insertBefore, removeChild } = Node.prototype;

Node.prototype.insertBefore = function <T extends Node>(this: Node, node: T, ref: Node | null): T {
  // A moved anchor usually still sits inside this parent, wrapped in something
  // new: insert before that wrapper to keep the order. A detached one climbs to
  // null, which appends.
  let anchor = ref;
  while (anchor && anchor.parentNode !== this) anchor = anchor.parentNode;
  return insertBefore.call(this, node, anchor) as T;
};

Node.prototype.removeChild = function <T extends Node>(this: Node, child: T): T {
  if (child.parentNode === this) return removeChild.call(this, child) as T;
  // Still remove it from wherever it ended up, or it would linger on screen.
  child.parentNode?.removeChild(child);
  return child;
};
