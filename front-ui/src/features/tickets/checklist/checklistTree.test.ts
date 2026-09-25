import { describe, expect, it } from 'vitest';
import type { TicketChecklistItemDTO } from '@/types/api';
import {
  buildChecklistTree,
  collectChecklistStats,
  parseChecklistTemplateText,
  planChecklistIndent,
  planChecklistInsertAfter,
  planChecklistOutdent,
  serializeChecklistTemplate,
} from './checklistTree';

const item = (id: string, parentID: string | null, position: number, isDone = false): TicketChecklistItemDTO => ({
  id,
  ticket_id: 't',
  parent_id: parentID,
  title: id.toUpperCase(),
  position,
  is_done: isDone,
  assignees: [],
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
});

describe('buildChecklistTree', () => {
  it('строит дерево по parent_id и position, сиротские пункты поднимает в корень', () => {
    const tree = buildChecklistTree([
      item('b', null, 1),
      item('a', null, 0),
      item('a2', 'a', 1),
      item('a1', 'a', 0, true),
      item('orphan', 'missing', 5),
    ]);
    expect(tree.map((node) => node.id)).toEqual(['a', 'b', 'orphan']);
    expect(tree[0].children.map((node) => node.id)).toEqual(['a1', 'a2']);
    expect(tree[0].children[0].depth).toBe(2);
    expect(collectChecklistStats([item('x', null, 0, true), item('y', null, 1)])).toEqual({ total: 2, done: 1 });
  });
});

describe('перемещение пунктов', () => {
  const tree = buildChecklistTree([
    item('a', null, 0),
    item('b', null, 1),
    item('b1', 'b', 0),
    item('c', null, 2),
  ]);

  it('сдвигает пункт вправо под предыдущего соседа', () => {
    expect(planChecklistIndent(tree, 'c')).toEqual({ parentID: 'b', orderedIDs: ['b1', 'c'] });
    expect(planChecklistIndent(tree, 'a')).toBeNull();
  });

  it('поднимает пункт на уровень выше сразу после родителя', () => {
    expect(planChecklistOutdent(tree, 'b1')).toEqual({ parentID: null, orderedIDs: ['a', 'b', 'b1', 'c'] });
    expect(planChecklistOutdent(tree, 'a')).toBeNull();
  });

  it('вставляет созданные пункты после указанного', () => {
    expect(planChecklistInsertAfter(tree, null, 'a', ['n1', 'n2'])).toEqual({
      parentID: null,
      orderedIDs: ['a', 'n1', 'n2', 'b', 'c'],
    });
  });

  it('не позволяет превысить максимальную вложенность', () => {
    const deep = buildChecklistTree([
      item('p', null, 0),
      item('q', null, 1),
      item('q1', 'q', 0),
      item('q11', 'q1', 0),
    ]);
    expect(planChecklistIndent(deep, 'q')).toBeNull();
  });
});

describe('текст шаблона', () => {
  it('разбирает отступы и маркеры списка и сериализует обратно', () => {
    const { nodes, errors } = parseChecklistTemplateText('Подготовка\n  - Проверить ФН\n\t\tГлубже\nЗапуск\n');
    expect(errors).toEqual([]);
    expect(nodes).toEqual([
      { title: 'Подготовка', children: [{ title: 'Проверить ФН', children: [{ title: 'Глубже' }] }] },
      { title: 'Запуск' },
    ]);
    expect(serializeChecklistTemplate(nodes)).toBe('Подготовка\n  Проверить ФН\n    Глубже\nЗапуск');
  });

  it('сообщает о превышении вложенности и не допускает прыжков через уровень', () => {
    const { nodes, errors } = parseChecklistTemplateText('A\n    B\n  C\n    D\n      E');
    expect(nodes[0].children?.map((node) => node.title)).toEqual(['B', 'C']);
    expect(errors).toHaveLength(1);
  });
});
