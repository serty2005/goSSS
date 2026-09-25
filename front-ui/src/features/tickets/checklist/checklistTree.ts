import type { TicketChecklistItemDTO } from '@/types/api';

// CHECKLIST_MAX_DEPTH совпадает с ограничением вложенности на backend.
export const CHECKLIST_MAX_DEPTH = 3;

export type ChecklistNode = TicketChecklistItemDTO & {
  depth: number;
  children: ChecklistNode[];
};

export type ChecklistStats = {
  total: number;
  done: number;
};

export type ChecklistMove = {
  parentID: string | null;
  orderedIDs: string[];
};

const compareItems = (a: TicketChecklistItemDTO, b: TicketChecklistItemDTO) => {
  if (a.position !== b.position) return a.position - b.position;
  return String(a.created_at).localeCompare(String(b.created_at));
};

export const buildChecklistTree = (items: TicketChecklistItemDTO[]): ChecklistNode[] => {
  const byParent = new Map<string, TicketChecklistItemDTO[]>();
  const ids = new Set(items.map((item) => item.id));
  items.forEach((item) => {
    const parentKey = item.parent_id && ids.has(item.parent_id) ? item.parent_id : '';
    const bucket = byParent.get(parentKey) || [];
    bucket.push(item);
    byParent.set(parentKey, bucket);
  });

  const visited = new Set<string>();
  const build = (parentKey: string, depth: number): ChecklistNode[] =>
    (byParent.get(parentKey) || [])
      .slice()
      .sort(compareItems)
      .filter((item) => {
        if (visited.has(item.id)) return false;
        visited.add(item.id);
        return true;
      })
      .map((item) => ({ ...item, depth, children: build(item.id, depth + 1) }));

  return build('', 1);
};

export const flattenChecklistTree = (nodes: ChecklistNode[]): ChecklistNode[] =>
  nodes.flatMap((node) => [node, ...flattenChecklistTree(node.children)]);

export const collectChecklistStats = (items: TicketChecklistItemDTO[]): ChecklistStats => ({
  total: items.length,
  done: items.filter((item) => item.is_done).length,
});

export const subtreeHeight = (node: ChecklistNode): number =>
  1 + node.children.reduce((max, child) => Math.max(max, subtreeHeight(child)), 0);

const findSiblings = (nodes: ChecklistNode[], parentID: string | null): ChecklistNode[] | null => {
  if (!parentID) return nodes;
  const parent = flattenChecklistTree(nodes).find((node) => node.id === parentID);
  return parent ? parent.children : null;
};

const findNode = (nodes: ChecklistNode[], itemID: string) =>
  flattenChecklistTree(nodes).find((node) => node.id === itemID) || null;

// planChecklistIndent делает пункт последним подпунктом предыдущего соседа.
export const planChecklistIndent = (nodes: ChecklistNode[], itemID: string): ChecklistMove | null => {
  const node = findNode(nodes, itemID);
  if (!node) return null;
  const siblings = findSiblings(nodes, node.parent_id);
  if (!siblings) return null;
  const index = siblings.findIndex((item) => item.id === itemID);
  if (index <= 0) return null;
  const newParent = siblings[index - 1];
  if (newParent.depth + subtreeHeight(node) > CHECKLIST_MAX_DEPTH) return null;
  return {
    parentID: newParent.id,
    orderedIDs: [...newParent.children.map((child) => child.id), itemID],
  };
};

// planChecklistOutdent переносит пункт на уровень родителя сразу после него.
export const planChecklistOutdent = (nodes: ChecklistNode[], itemID: string): ChecklistMove | null => {
  const node = findNode(nodes, itemID);
  if (!node || !node.parent_id) return null;
  const parent = findNode(nodes, node.parent_id);
  if (!parent) return null;
  const parentSiblings = findSiblings(nodes, parent.parent_id);
  if (!parentSiblings) return null;
  const orderedIDs: string[] = [];
  parentSiblings.forEach((sibling) => {
    orderedIDs.push(sibling.id);
    if (sibling.id === parent.id) {
      orderedIDs.push(itemID);
    }
  });
  return { parentID: parent.parent_id, orderedIDs };
};

// planChecklistInsertAfter ставит новые пункты сразу после указанного соседа.
export const planChecklistInsertAfter = (
  nodes: ChecklistNode[],
  parentID: string | null,
  afterID: string,
  createdIDs: string[],
): ChecklistMove | null => {
  const siblings = findSiblings(nodes, parentID);
  if (!siblings) return null;
  const created = new Set(createdIDs);
  const orderedIDs: string[] = [];
  siblings.forEach((sibling) => {
    if (created.has(sibling.id)) return;
    orderedIDs.push(sibling.id);
    if (sibling.id === afterID) {
      orderedIDs.push(...createdIDs);
    }
  });
  if (!orderedIDs.includes(createdIDs[0])) return null;
  return { parentID, orderedIDs };
};

export type ChecklistTemplateNode = {
  title: string;
  children?: ChecklistTemplateNode[];
};

const INDENT_UNIT = 2;

const lineDepth = (line: string) => {
  const leading = line.match(/^[\t ]*/)?.[0] || '';
  const width = leading.replace(/\t/g, ' '.repeat(INDENT_UNIT)).length;
  return Math.floor(width / INDENT_UNIT) + 1;
};

// parseChecklistTemplateText превращает текст с отступами (2 пробела или Tab на уровень) в дерево пунктов.
export const parseChecklistTemplateText = (text: string): { nodes: ChecklistTemplateNode[]; errors: string[] } => {
  const roots: ChecklistTemplateNode[] = [];
  const stack: Array<{ depth: number; node: ChecklistTemplateNode }> = [];
  const errors: string[] = [];

  String(text || '').replace(/\r\n/g, '\n').split('\n').forEach((rawLine, index) => {
    const title = rawLine.trim().replace(/^[-*•]\s+/, '').trim();
    if (!title) return;
    let depth = lineDepth(rawLine);
    const parentDepth = stack.length > 0 ? stack[stack.length - 1].depth : 0;
    if (depth > parentDepth + 1) {
      depth = parentDepth + 1;
    }
    if (depth > CHECKLIST_MAX_DEPTH) {
      errors.push(`Строка ${index + 1}: допускается не более ${CHECKLIST_MAX_DEPTH} уровней вложенности`);
      depth = CHECKLIST_MAX_DEPTH;
    }
    while (stack.length > 0 && stack[stack.length - 1].depth >= depth) {
      stack.pop();
    }
    const node: ChecklistTemplateNode = { title };
    if (stack.length === 0) {
      roots.push(node);
    } else {
      const parent = stack[stack.length - 1].node;
      parent.children = [...(parent.children || []), node];
    }
    stack.push({ depth, node });
  });

  return { nodes: roots, errors };
};

export const serializeChecklistTemplate = (nodes: ChecklistTemplateNode[], depth = 1): string =>
  nodes
    .map((node) => {
      const line = `${' '.repeat((depth - 1) * INDENT_UNIT)}${node.title}`;
      const children = node.children?.length ? `\n${serializeChecklistTemplate(node.children, depth + 1)}` : '';
      return line + children;
    })
    .join('\n');

export const countChecklistTemplateNodes = (nodes: ChecklistTemplateNode[] = []): number =>
  nodes.reduce((sum, node) => sum + 1 + countChecklistTemplateNodes(node.children), 0);
