#!/usr/bin/env python3
"""
Скрипт для сбора файлов бэкэнда и фронтэнда по отдельности.
Собирает только codebase системы, исключая системные довески.
Создаёт текстовые файлы с содержимым кода.
"""

import os
import fnmatch
from pathlib import Path
from datetime import datetime
import argparse


# Паттерны файлов и директорий для исключения
EXCLUDE_PATTERNS = {
    # Node.js
    'node_modules',
    '.vite',
    '.eslintcache',
    'dist',
    'build',
    'coverage',
    '.next',
    '.nuxt',
    
    # Python
    '__pycache__',
    '*.pyc',
    '*.pyo',
    '*.pyd',
    '.Python',
    'venv',
    'env',
    '.venv',
    '.telethon',
    
    # Go
    'bin',
    '*.exe',
    '*.exe~',
    '*.dll',
    '*.so',
    '*.dylib',
    
    # IDE
    '.vscode',
    '.idea',
    '*.swp',
    '*.swo',
    '*~',
    
    # OS
    '.DS_Store',
    'Thumbs.db',
    'desktop.ini',
    
    # Logs
    '*.log',
    'logs',
    
    # Temporary
    '*.tmp',
    '*.bak',
    '*.cache',
    
    # Git
    '.git',
    '.gitignore',
    
    # Env & Secrets
    '.env',
    '.env.*',
    '*-service-account.json',
    '*firebase-adminsdk*.json',
    '*firebase-key.json',
    
    # Documentation artifacts
    '*.pdf',
    '*.docx',
    
    # Docker
    'Dockerfile',
    '.dockerignore',
    'cloudbuild*.yaml',
    'cloudbuild*.yml',
    '.gcloudignore',
    
    # Бинарники (исполняемые файлы без расширения)
    # Будет проверяться отдельно
}


def should_exclude(path: Path, root: Path) -> bool:
    """Проверяет, нужно ли исключить файл или директорию."""
    rel_path = path.relative_to(root)
    path_str = str(rel_path)
    name = path.name
    
    # Проверка по имени файла/директории
    for pattern in EXCLUDE_PATTERNS:
        # Если паттерн содержит wildcard, используем fnmatch
        if '*' in pattern or '?' in pattern:
            if fnmatch.fnmatch(name, pattern) or fnmatch.fnmatch(path_str, pattern):
                return True
        elif pattern.startswith('*'):
            # Паттерн с wildcard в начале
            if name.endswith(pattern[1:]) or path_str.endswith(pattern[1:]):
                return True
        elif pattern.endswith('*'):
            # Паттерн с wildcard в конце
            if name.startswith(pattern[:-1]) or path_str.startswith(pattern[:-1]):
                return True
        else:
            # Точное совпадение
            if name == pattern or pattern in path_str.split(os.sep):
                return True
    
    # Проверка расширения для паттернов типа *.ext
    for pattern in EXCLUDE_PATTERNS:
        if pattern.startswith('*.') and path.is_file():
            if name.endswith(pattern[1:]):
                return True
    
    # Исключаем бинарники (исполняемые файлы без расширения)
    if path.is_file() and not path.suffix:
        try:
            if path.stat().st_mode & 0o111:
                return True
        except:
            pass
    
    # Исключаем известные бинарники по имени
    binary_names = {'server', 'verified-proxy'}
    if name in binary_names:
        return True
    
    return False


def is_text_file(file_path: Path) -> bool:
    """Проверяет, является ли файл текстовым."""
    text_extensions = {
        '.go', '.ts', '.tsx', '.js', '.jsx', '.json', '.yaml', '.yml',
        '.md', '.txt', '.sh', '.ps1', '.sql', '.css', '.html', '.xml',
        '.rules', '.mod', '.sum', '.mjs', '.config.js', '.config.ts',
        '.toml', '.ini', '.conf', '.env.example'
    }
    
    if file_path.suffix.lower() in text_extensions:
        return True
    
    # Файлы без расширения могут быть текстовыми (например, Makefile)
    # Но исключаем известные бинарники
    if not file_path.suffix:
        binary_names = {'server', 'verified-proxy'}
        if file_path.name in binary_names:
            return False
        # Если файл исполняемый и большой (>100KB), вероятно бинарник
        try:
            stat = file_path.stat()
            if stat.st_mode & 0o111 and stat.st_size > 100 * 1024:
                return False
        except:
            pass
        return True
    
    return False


def collect_files_to_text(source_dir: Path, output_file: Path):
    """Собирает файлы из source_dir в текстовый файл output_file."""
    files_collected = 0
    total_size = 0
    
    with open(output_file, 'w', encoding='utf-8') as out:
        for root, dirs, files in os.walk(source_dir):
            root_path = Path(root)
            
            # Фильтруем директории для обхода
            dirs[:] = [d for d in dirs if not should_exclude(root_path / d, source_dir)]
            
            for file in sorted(files):
                file_path = root_path / file
                
                if should_exclude(file_path, source_dir):
                    continue
                
                # Собираем только текстовые файлы и файлы с кодом
                if not is_text_file(file_path):
                    continue
                
                # Вычисляем относительный путь
                rel_path = file_path.relative_to(source_dir)
                
                try:
                    # Читаем содержимое файла
                    try:
                        content = file_path.read_text(encoding='utf-8')
                    except UnicodeDecodeError:
                        # Пробуем другие кодировки
                        try:
                            content = file_path.read_text(encoding='latin-1')
                        except Exception:
                            print(f"Предупреждение: не удалось прочитать {file_path} (бинарный файл?)")
                            continue
                    except Exception as e:
                        print(f"Предупреждение: ошибка при чтении {file_path}: {e}")
                        continue
                    
                    # Записываем заголовок с именем файла и путём
                    out.write(f"\n{'='*80}\n")
                    out.write(f"Файл: {file_path.name}\n")
                    out.write(f"Путь: {rel_path}\n")
                    out.write(f"{'='*80}\n\n")
                    
                    # Записываем содержимое файла
                    out.write(content)
                    out.write("\n")
                    
                    files_collected += 1
                    total_size += file_path.stat().st_size
                    
                except Exception as e:
                    print(f"Предупреждение: не удалось обработать {file_path}: {e}")
    
    return files_collected, total_size


def main():
    parser = argparse.ArgumentParser(
        description='Собирает файлы бэкэнда и фронтэнда по отдельности в текстовые файлы'
    )
    parser.add_argument(
        '--output-dir',
        type=str,
        default='./collected',
        help='Директория для сохранения текстовых файлов (по умолчанию: ./collected)'
    )
    parser.add_argument(
        '--backend-only',
        action='store_true',
        help='Собрать только бэкэнд'
    )
    parser.add_argument(
        '--frontend-only',
        action='store_true',
        help='Собрать только фронтэнд'
    )
    
    args = parser.parse_args()
    
    # Определяем корневую директорию проекта
    script_dir = Path(__file__).parent
    project_root = script_dir.parent
    
    backend_dir = project_root / 'backend'
    frontend_dir = project_root / 'web'
    
    # Проверяем существование директорий
    if not args.frontend_only and not backend_dir.exists():
        print(f"Ошибка: директория бэкэнда не найдена: {backend_dir}")
        return 1
    
    if not args.backend_only and not frontend_dir.exists():
        print(f"Ошибка: директория фронтэнда не найдена: {frontend_dir}")
        return 1
    
    # Создаем директорию для выходных файлов
    output_dir = Path(args.output_dir).resolve()
    output_dir.mkdir(parents=True, exist_ok=True)
    
    timestamp = datetime.now().strftime('%Y%m%d_%H%M%S')
    
    results = []
    
    # Собираем бэкэнд
    if not args.frontend_only:
        backend_output = output_dir / f'backend_{timestamp}.txt'
        print(f"Сборка бэкэнда из {backend_dir}...")
        files_count, total_size = collect_files_to_text(backend_dir, backend_output)
        size_mb = total_size / (1024 * 1024)
        print(f"✓ Бэкэнд собран: {files_count} файлов, {size_mb:.2f} MB")
        print(f"  Сохранено в: {backend_output}")
        results.append(('backend', backend_output, files_count, size_mb))
    
    # Собираем фронтэнд
    if not args.backend_only:
        frontend_output = output_dir / f'frontend_{timestamp}.txt'
        print(f"Сборка фронтэнда из {frontend_dir}...")
        files_count, total_size = collect_files_to_text(frontend_dir, frontend_output)
        size_mb = total_size / (1024 * 1024)
        print(f"✓ Фронтэнд собран: {files_count} файлов, {size_mb:.2f} MB")
        print(f"  Сохранено в: {frontend_output}")
        results.append(('frontend', frontend_output, files_count, size_mb))
    
    # Итоговая информация
    print("\n" + "=" * 60)
    print("Итоги сборки:")
    print("=" * 60)
    for name, path, files, size in results:
        print(f"{name:12} | {files:6} файлов | {size:8.2f} MB | {path.name}")
    print("=" * 60)
    
    return 0


if __name__ == '__main__':
    exit(main())

