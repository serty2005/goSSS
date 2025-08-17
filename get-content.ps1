# Путь к директории, которую сканируем (по умолчанию текущая)
$rootPath = Get-Location

# Путь к выходному файлу
$outputFile = Join-Path $rootPath "go_files_dump.txt"

# Удаляем старый файл, если он существует
if (Test-Path $outputFile) {
    Remove-Item $outputFile -Force
}

# Получаем все .go файлы рекурсивно
$goFiles = Get-ChildItem -Path $rootPath -Recurse -Filter "*.go" -File

foreach ($file in $goFiles) {
    # Получаем относительный путь от корня
    $relativePath = $file.FullName.Replace($rootPath, "").TrimStart("\").Replace("\", "/")

    # Заголовок блока
    $startTag = "===== START $($file.Name) ====="
    $endTag =   "===== END $($file.Name) ====="

    # Содержимое файла
    $fileContent = Get-Content $file.FullName -Raw

    # Собираем финальный блок
    $block = @"
$relativePath
$startTag
go ```
$fileContent
go ```
$endTag

"@

    # Добавляем в итоговый файл
    Add-Content -Path $outputFile -Value $block
}

Write-Host "Все .go-файлы успешно собраны в $outputFile"
