#!/usr/bin/env python3
"""
Скрипт для упаковки всех .go файлов проекта в один .py файл
с отображением дерева проекта и экранированным содержимым файлов.
"""

import os
import sys
import json


def escape_content(content: str) -> str:
    """
    Экранирует содержимое файла для корректного помещения в Python-строку.
    Используем json.dumps для максимальной безопасности и читаемости.
    """
    return json.dumps(content, ensure_ascii=False)


def collect_go_files(root_dir: str):
    """
    Рекурсивно собирает пути ко всем .go файлам в проекте.
    """
    go_files = []
    for dirpath, _, filenames in os.walk(root_dir):
        for file in filenames:
            if file.endswith(".go"):
                full_path = os.path.join(dirpath, file)
                go_files.append(os.path.normpath(full_path))
    return sorted(go_files)


def build_tree(root_dir: str) -> str:
    """
    Создаёт строковое представление дерева проекта.
    """
    tree_lines = []

    def walk(dir_path: str, prefix: str = ""):
        entries = sorted(os.listdir(dir_path))
        for idx, entry in enumerate(entries):
            path = os.path.join(dir_path, entry)
            connector = "└── " if idx == len(entries) - 1 else "├── "
            tree_lines.append(f"{prefix}{connector}{entry}")
            if os.path.isdir(path):
                new_prefix = prefix + ("    " if idx == len(entries) - 1 else "│   ")
                walk(path, new_prefix)

    tree_lines.append(".")
    walk(root_dir)
    return "\n".join(tree_lines)


def write_to_py(go_files, tree_str, output_file):
    """
    Записывает дерево проекта и содержимое .go файлов в один .py файл.
    """
    with open(output_file, "w", encoding="utf-8") as f:
        # Заголовок
        f.write("# -*- coding: utf-8 -*-\n")
        f.write("# Этот файл сгенерирован автоматически.\n")
        f.write("# Содержит дерево проекта и все .go файлы в экранированном виде.\n\n")

        # Дерево проекта
        f.write("project_tree = '''\n")
        f.write(tree_str)
        f.write("\n'''\n\n")

        # Словарь файлов
        f.write("project_files = {\n")
        for path in go_files:
            rel_path = os.path.relpath(path)
            with open(path, "r", encoding="utf-8", errors="ignore") as src:
                content = src.read()
            escaped_content = escape_content(content)
            f.write(f'    "{rel_path}": {escaped_content},\n')
        f.write("}\n\n")

        # Тестовый вывод
        f.write("if __name__ == '__main__':\n")
        f.write("    print('=== Дерево проекта ===')\n")
        f.write("    print(project_tree)\n")
        f.write("    print('\\n=== Список файлов ===')\n")
        f.write("    for name in project_files:\n")
        f.write("        print(f'- {name}')\n")


def main():
    root_dir = "."
    output_file = "go_project_dump.py"

    if len(sys.argv) > 1:
        output_file = sys.argv[1]

    go_files = collect_go_files(root_dir)
    tree_str = build_tree(root_dir)
    write_to_py(go_files, tree_str, output_file)
    print(f"Собрано {len(go_files)} файлов. Результат в {output_file}")


if __name__ == "__main__":
    main()
