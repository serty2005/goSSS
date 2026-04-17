# План развития `iiko-syrve-rms-adapter`

## Цель

Расширить существующий адаптер `iiko-syrve-rms-adapter`, чтобы он умел:

- определять адрес RMS-сервера из `config.xml`;
- читать `CRMid` из `cash-server.log` фронта iiko;
- собирать список установленных плагинов `iikoFront` с версиями;
- выполнять мягкую остановку фронта через `WM_CLOSE`.
- проверять источники автозапуска фронта в `shell:startup`, `shell:common startup` и планировщике задач;
- добавлять указанный способ автозапуска фронта;
- вычитывать все настройки фронта из `config.xml` в формате, пригодном для будущего редактирования.

Текущий этап ограничен подготовкой плана реализации. Реализация под `Syrve` для логов и путей откладывается на следующий шаг, но архитектура должна остаться расширяемой под оба продукта.

## Что уже есть в репозитории

- В `agent/internal/iikosyrverms` уже реализованы:
  - поиск `%AppData%` и fallback по `C:\Users\*\AppData\Roaming`;
  - детекция `iiko/syrve`;
  - поиск `config.xml`;
  - извлечение `serverUrl`;
  - контракт адаптера с `describe`, `health`, `run`;
  - поддержка только `task_type = "collect"`.
- В `agent/internal/hostinfo/collector_windows.go` уже есть похожая логика поиска `config.xml` по нескольким пользовательским профилям.
- Внутри текущего плана нужно реализовать недостающие подсистемы прямо в адаптере, без опоры на внешние проекты:
  - чтение `serverUrl` из `iiko\\cashserver\\config.xml` и `syrve\\cashserver\\config.xml`;
  - чтение `manifest.xml` и основной DLL плагина;
  - определение версии DLL по version resource;
  - мягкую остановку процесса через `WM_CLOSE`;
  - работу с папками Startup, ярлыками и `schtasks`.

## Предлагаемый scope второй версии

### Поддерживаемый продукт

- Полная функциональность реализуется для `iiko`.
- Для `syrve` сохраняется текущая детекция `config.xml`, но:
  - чтение `CRMid` из логов пока не включается;
  - сбор плагинов и мягкая остановка можно проектировать с точками расширения, но без обещания полной поддержки в этой итерации;
  - проверка автозапуска должна сразу учитывать и `iiko`, и `Syrve`, потому что поиск идёт по ярлыкам и задачам запуска фронта;
  - чтение всех настроек из `config.xml` проектируется общим для `iiko` и `Syrve`, если структура файла совпадает.

### Рекомендуемый контракт адаптера

С учётом уже существующего runtime в `goSSS` новые действия лучше не выносить в отдельную top-level команду, а добавить новые `task_type` в `run`.

Предлагаемые значения:

- `collect` — собрать RMS-данные, `CRMid`, сведения о фронте и список плагинов;
- `soft_shutdown_front` — мягко закрыть окно фронта через `WM_CLOSE`;
- `inspect_autorun` — проверить все поддержанные источники автозапуска и найти запуск `iikoFront` или `Syrve Front`;
- `ensure_autorun` — добавить указанный способ автозапуска;
- `read_front_config` — вычитать все доступные настройки фронта из `config.xml`.

На будущее стоит зарезервировать направление для:

- `write_front_config` — изменить выбранные настройки в `config.xml`.

### Предлагаемый результат `task_type = "collect"`

Минимально вернуть:

```json
{
  "status": "success",
  "result": {
    "software_type": "iiko",
    "rms_url": "https://example/resto/",
    "crm_id": "1740537",
    "plugins": [
      {
        "name": "Transport",
        "api_version": "V9Preview7",
        "version": "9.7.20",
        "directory": "Resto.Front.Api.Transport.V9Preview7"
      }
    ]
  }
}
```

В `details` дополнительно вернуть:

- путь к `config.xml`;
- путь к найденному `cash-server.log`;
- путь к каталогу плагинов;
- путь к исполняемому файлу фронта;
- снимок основных путей фронта;
- предупреждения и причины partial-результата.

### Предлагаемый результат `task_type = "soft_shutdown_front"`

```json
{
  "status": "success",
  "result": {
    "software_type": "iiko",
    "process_name": "iikoFront.Net.exe",
    "matched_pids": [1234],
    "close_sent": true
  }
}
```

Если процесс не найден, лучше вернуть прикладной `failed` с понятным `error_code`, а не считать это ошибкой JSON-контракта.

### Предлагаемый результат `task_type = "inspect_autorun"`

```json
{
  "status": "success",
  "result": {
    "software_type": "iiko",
    "entries": [
      {
        "source": "startup_user",
        "path": "C:\\Users\\demo\\AppData\\Roaming\\Microsoft\\Windows\\Start Menu\\Programs\\Startup\\iikoFront.lnk",
        "target_path": "C:\\Program Files\\iiko\\iikoRMS\\Front.Net\\iikoFront.Net.exe",
        "arguments": "",
        "matches_front": true
      }
    ]
  }
}
```

### Предлагаемый результат `task_type = "ensure_autorun"`

```json
{
  "status": "success",
  "result": {
    "method": "startup_user",
    "created": true
  }
}
```

### Предлагаемый результат `task_type = "read_front_config"`

```json
{
  "status": "success",
  "result": {
    "software_type": "iiko",
    "source_file": "C:\\Users\\demo\\AppData\\Roaming\\iiko\\cashserver\\config.xml",
    "settings": [
      {
        "path": "/configuration/serverUrl",
        "value": "https://example/resto/",
        "attributes": {}
      }
    ]
  }
}
```

## План реализации

### 1. Уточнить и расширить контракт адаптера

- Обновить `contract/types.go`:
  - добавить константу `TaskTypeSoftShutdownFront`;
  - добавить константы `TaskTypeInspectAutorun`, `TaskTypeEnsureAutorun`, `TaskTypeReadFrontConfig`;
  - расширить `RunResult` новыми полями или перейти на вложенную доменную структуру результата;
  - добавить модели `PluginInfo`, `AutorunEntry`, `ConfigSetting`;
  - добавить payload-модель для `ensure_autorun`.
- Обновить `cli/app.go`:
  - принимать как минимум `collect`, `soft_shutdown_front`, `inspect_autorun`, `ensure_autorun`, `read_front_config`;
  - маршрутизировать их в отдельные handler/service-методы.
- Обновить `describe`:
  - дополнить `capabilities` новыми возможностями, например `read-crm-id`, `list-plugins`, `soft-shutdown-front`, `inspect-autorun`, `ensure-autorun`, `read-front-config`.

### 2. Перестроить внутреннюю модель scan-результата

- Расширить `domain.ScanReport` полями:
  - `CRMID string`
  - `CashServerLog string`
  - `FrontExecutable string`
  - `PluginsRoot string`
  - `Plugins []PluginInfo`
  - `AutorunEntries []AutorunEntry`
  - `ConfigSnapshot ConfigSnapshot`
- Добавить доменные модели:
  - `PluginInfo`
  - `FrontInstallation`
  - `AutorunEntry`
  - `ConfigSetting`
  - `ConfigSnapshot`
  - при необходимости `LogCandidate`, `AutorunCandidate`, `ShortcutTarget`.
- Разделить результат на два слоя:
  - общий discovery-слой с найденными путями;
  - task-слой, который превращает discovery в JSON-ответ для `collect`, `soft_shutdown_front`, `inspect_autorun`, `ensure_autorun`, `read_front_config`.

### 3. Расширить discovery: найти front root, log и plugins root

- Не ограничиваться только `%AppData%`.
- Добавить отдельный модуль поиска установки фронта, который проверяет:
  - `InstallLocation` из установленного ПО;
  - известные каталоги `Program Files`;
  - существование `iikoFront.Net.exe`.
- Для iiko закладывать несколько кандидатов:
  - `C:\Program Files\iiko\iikoFront.Net.exe`
  - `C:\Program Files (x86)\iiko\iikoFront.Net.exe`
  - `C:\Program Files\iiko\iikoRMS\Front.Net\iikoFront.Net.exe`
  - `C:\Program Files (x86)\iiko\iikoRMS\Front.Net\iikoFront.Net.exe`
- Из найденного front root выводить кандидаты:
  - каталога `Plugins`;
  - исполняемого файла фронта.
- Для `cash-server.log` заложить поиск по набору кандидатов:
  - рядом с известными cashserver-каталогами в `%AppData%`;
  - в подкаталогах логов внутри дерева iiko front/cashserver;
  - по правилу «самый свежий подходящий файл среди известных путей».

Важно: точный каталог `cash-server.log` в текущих исходниках не подтверждён. В реализации нужно сперва оформить список кандидатов и покрыть его тестами на фикстурах.

### 4. Спроектировать чтение всех настроек из `config.xml`

- Не ограничиваться чтением только `serverUrl`.
- Добавить отдельный reader, который строит два представления:
  - иерархическое дерево XML для будущего редактирования;
  - плоский список настроек вида `path/value/attributes`.
- В настройку включать:
  - путь узла;
  - текстовое значение;
  - набор атрибутов;
  - признак повторяемого элемента, если встречаются одноимённые узлы.
- Сразу заложить сохранение метаданных, которые пригодятся в будущем для записи:
  - исходный порядок узлов;
  - имя элемента;
  - родительский путь;
  - исходный файл.
- Для первой версии изменения файла не выполнять, только чтение и сериализацию ответа.

#### Что должно быть реализовано внутри адаптера

- `parser/config_reader.go`
  - читает XML;
  - строит DOM-представление или эквивалентную древовидную модель;
  - обходит все элементы в глубину;
  - для каждого элемента формирует запись `ConfigSetting`.
- `domain.ConfigSetting` должен содержать:
  - `path`
  - `name`
  - `value`
  - `attributes`
  - `parent_path`
  - `index`
  - `repeated`
- Путь узла формировать в виде XPath-подобной строки:
  - `/configuration/serverUrl`
  - `/configuration/endpoints/endpoint[0]/url`
- Для повторяющихся элементов сохранять индекс в пределах родителя.
- Атрибуты хранить как map с исходными именами.
- Пустой текстовый узел не пропускать, если у него есть атрибуты или дочерние элементы.
- В `details` полезно вернуть:
  - `source_file`
  - `root_element`
  - `settings_count`
  - `has_repeated_nodes`
- Для совместимости с будущей записью нужно сохранить возможность восстановить:
  - исходный порядок элементов;
  - точное имя тега;
  - все атрибуты;
  - принадлежность к родительскому узлу.

### 5. Реализовать чтение `CRMid` из `cash-server.log`

- Добавить отдельный parser для логов cash server.
- Для первой версии под iiko использовать выражение, устойчивое к пробелам:
  - `ID организации\\s*:\\s*(\\d+)`
- Читать лог с конца либо построчно с ограничением по объёму, чтобы не тянуть целиком очень большие файлы.
- Возвращать:
  - `crm_id`;
  - исходный путь лога;
  - diagnostic reason, если файл найден, но строка не найдена.
- Статус `collect` рекомендую делать:
  - `success`, если есть `rms_url`, `crm_id` и скан плагинов завершился без критических ошибок;
  - `partial`, если найден iiko, но один из источников данных недоступен;
  - `failed`, если поддерживаемое ПО не найдено вообще.

### 6. Реализовать локальный сбор установленных плагинов

#### Предлагаемый алгоритм

- Найти каталог `Plugins` в установленном iikoFront.
- Просканировать только директории первого уровня.
- Для каждой директории:
  - прочитать `manifest.xml`, если он есть;
  - определить основную DLL плагина;
  - получить имя плагина через нормализацию имени DLL/manifest;
  - получить `api_version` из `manifest.xml`, если есть;
  - получить `version` из version resource основной DLL;
  - если version resource нет, попробовать извлечь версию из имени каталога;
  - сохранить имя каталога как `directory`.
- Отсортировать результат детерминированно по имени и версии.

#### Что должно быть реализовано внутри адаптера

- Новый модуль `pluginscanner`:
  - `scan(root string) ([]PluginInfo, []string, error)`
  - `readManifest(dir string) (PluginManifest, bool, error)`
  - `detectPrimaryDLL(dir string, manifest PluginManifest) (string, error)`
  - `readDLLVersion(path string) (string, error)`
  - `normalizePluginName(raw string) string`
  - `normalizePluginToken(raw string) string`
- `normalizePluginName` должен:
  - убрать известные префиксы вроде `Resto.Front.Api.`, `Plugin.Front.Api.`, `Plugin.Front.`, `Resto.Front.`;
  - убрать хвостовые `.` и `-`;
  - вернуть человекочитаемое имя плагина.
- `readManifest` должен читать локальный `manifest.xml` и извлекать:
  - `FileName`
  - `ApiVersion`
- `detectPrimaryDLL` должен:
  - сначала использовать `FileName` из manifest;
  - если manifest отсутствует или в нём нет DLL, искать `.dll` в каталоге;
  - выбирать DLL по совпадению токенов с именем каталога и именем плагина.
- `readDLLVersion` должен вычитать file version из ресурсов PE-файла.
- `PluginInfo` должен содержать:
  - `name`
  - `api_version`
  - `version`
  - `directory`
  - `manifest_file`
  - `dll_file`
- Если версия не найдена, это не должно ломать весь scan: плагин попадает в результат с пустым `version` и предупреждением.

#### Почему версия должна браться из DLL

- Имя каталога может содержать только API-версию или вообще не содержать версию сборки.
- `manifest.xml` даёт `FileName` и `ApiVersion`, но не всегда версию.
- Самый надёжный локальный источник версии установленного плагина — file version основной DLL.

### 7. Реализовать мягкую остановку фронта

- Добавить отдельный модуль наподобие `shutdown` или `frontcontrol`.
- Реализовать встроенную windows-only логику:
  - поиск PID по имени процесса;
  - обход окон через `EnumWindows`;
  - фильтр по PID и видимости окна;
  - отправка `WM_CLOSE`.
- Для первой версии целевой процесс:
  - `iikoFront.Net.exe`
- В `result/details` полезно вернуть:
  - список найденных PID;
  - количество окон, куда был отправлен `WM_CLOSE`;
  - факт, что команда только инициировала закрытие, а не гарантировала завершение процесса.

#### Что должно быть реализовано внутри адаптера

- `shutdown/windows.go`
  - `findPIDsByProcessPrefix(name string) ([]uint32, error)`
  - `sendCloseToVisibleWindows(pid uint32) (int, error)`
  - `softShutdown(processName string) (ShutdownResult, error)`
- Источник списка процессов:
  - `tasklist /NH /FO CSV`, если нужен быстрый и понятный вариант;
  - либо WinAPI, если захочется избежать внешней команды.
- `sendCloseToVisibleWindows` должен:
  - вызвать `EnumWindows`;
  - через `GetWindowThreadProcessId` проверить PID;
  - через `IsWindowVisible` отфильтровать скрытые окна;
  - отправить `PostMessageW(hwnd, WM_CLOSE, 0, 0)`.
- Результат должен содержать:
  - `process_name`
  - `matched_pids`
  - `windows_closed`
  - `close_sent`

### 8. Реализовать проверку автозапуска фронта

- Добавить отдельный модуль `autorun` или `startup`.
- Источники проверки:
  - папка `shell:startup`;
  - папка `shell:common startup`;
  - планировщик задач.
- Для Startup-папок:
  - найти `.lnk`-файлы;
  - разрешить target path и arguments;
  - определить, ссылается ли ярлык на `iikoFront.Net.exe`, `Front.Net.exe` или другой известный exe фронта;
  - вернуть источник, путь ярлыка, целевой exe, аргументы и признак совпадения.
- Для планировщика:
  - перечислить задачи;
  - извлечь action executable и arguments;
  - определить, запускает ли задача фронт `iiko` или `Syrve`.
- Сделать полный scanner, потому что нам нужен не поиск по одному точному exe, а инвентаризация всех подходящих источников автозапуска.

#### Что должно быть реализовано внутри адаптера

- `autorun/startup_paths_windows.go`
  - возвращает пути user/common startup:
    - `%APPDATA%\\Microsoft\\Windows\\Start Menu\\Programs\\Startup`
    - `%ProgramData%\\Microsoft\\Windows\\Start Menu\\Programs\\StartUp`
- `autorun/shortcut_windows.go`
  - читает `.lnk` через COM `WScript.Shell` или через WinAPI shell link;
  - возвращает `target_path`, `arguments`, `working_dir`.
- `autorun/scheduler_windows.go`
  - перечисляет задачи через `schtasks /Query /V /FO CSV` или XML-выгрузку;
  - извлекает имя задачи и команду запуска;
  - для продвинутого варианта читает XML задачи, если CSV недостаточен для точного action.
- `autorun/inspect.go`
  - объединяет результаты из Startup и scheduler;
  - сопоставляет exe с известными именами фронта;
  - определяет `software_type` по пути и имени exe.
- `AutorunEntry` должен содержать:
  - `source`
  - `path`
  - `target_path`
  - `arguments`
  - `working_dir`
  - `task_name`
  - `matches_front`
  - `software_type`

### 9. Реализовать добавление указанного способа автозапуска

- Для `ensure_autorun` предусмотреть payload примерно такого вида:

```json
{
  "method": "startup_user",
  "software_type": "iiko"
}
```

- Поддержанные методы:
  - `startup_user`
  - `startup_common`
  - `scheduler`
- Логика должна:
  - определить актуальный exe фронта;
  - определить рабочую директорию;
  - восстановить нужные аргументы запуска, если они обязательны для конкретной установки;
  - создать ярлык или задачу;
  - вернуть созданный путь или имя задачи.
- Для Startup-папок:
  - использовать `.lnk`;
  - имя ярлыка сделать детерминированным и понятным.
- Для планировщика:
  - использовать детерминированное имя задачи;
  - создавать или обновлять задачу идемпотентно.
- Перед созданием записи полезно сначала запускать внутреннюю проверку существующих источников, чтобы не плодить дубликаты.

#### Что должно быть реализовано внутри адаптера

- `AutorunEnsurePayload`:
  - `method`
  - `software_type`
  - `arguments`
  - `task_name`
  - `shortcut_name`
- Если `software_type` не передан, адаптер сам определяет его по найденной установке.
- Для `startup_user` и `startup_common`:
  - вычислить путь ярлыка;
  - создать `.lnk` с target на exe фронта;
  - сохранить аргументы, если они обязательны.
- Для `scheduler`:
  - сформировать XML-задачу;
  - указать exe, arguments и working dir;
  - создать или обновить задачу через `schtasks /Create /TN ... /XML ... /F`.
- Логика идемпотентности:
  - если уже существует запись с тем же target и arguments, вернуть `created=false`, `updated=false`;
  - если запись есть, но отличается, вернуть `updated=true`.
- Результат должен содержать:
  - `method`
  - `created`
  - `updated`
  - `path`
  - `task_name`

### 10. Пересобрать orchestration сервиса

- Текущий `service.Scan()` возвращает только RMS-данные.
- Его нужно разделить на более явные шаги:
  - `DetectSoftware`
  - `ResolveConfig`
  - `ReadFrontConfig`
  - `ResolveCashServerLog`
  - `ResolveFrontInstallation`
  - `ReadCRMID`
  - `ScanPlugins`
  - `SoftShutdownFront`
  - `InspectAutorun`
  - `EnsureAutorun`
- Это позволит:
  - не гонять сбор плагинов при `health`, если это слишком дорого;
  - отдельно вызывать только чтение `config.xml` без остального тяжёлого сбора;
  - отдельно тестировать каждый источник данных;
  - позже без боли подключить `Syrve`.

### 11. Тесты

#### Unit-тесты

- parser `CRMid`:
  - строка есть;
  - строка отсутствует;
  - лишние пробелы и табы;
  - несколько совпадений, выбирается корректное.
- discovery log path:
  - несколько кандидатов;
  - выбор самого свежего;
  - отсутствие доступа к части путей.
- plugins scanner:
  - каталог с `manifest.xml` и DLL;
  - каталог без manifest, но с распознаваемой DLL;
  - каталог без версии у DLL;
  - мусорные каталоги внутри `Plugins`.
- shutdown:
  - unit-тесты на слой отбора и сериализации результата;
  - системные WinAPI-вызовы изолировать интерфейсом или windows-only wrapper.
- autorun inspect:
  - ярлык в user startup;
  - ярлык в common startup;
  - задача планировщика с запуском фронта;
  - посторонние ярлыки и задачи не попадают в результат.
- autorun ensure:
  - создание ярлыка в user startup;
  - создание ярлыка в common startup;
  - создание задачи планировщика;
  - повторный запуск идемпотентен.
- read_front_config:
  - простые узлы;
  - атрибуты;
  - повторяющиеся элементы;
  - пустые значения;
  - сохранение корректных путей настроек.

#### Фикстуры

- Добавить testdata для:
  - `%AppData%` дерева с `config.xml`;
  - примера `cash-server.log`;
  - каталога `Plugins` с несколькими фейковыми плагинами;
  - Startup-папок с ярлыками-заглушками;
  - примеров XML-конфигов с разными структурами.

#### Smoke-проверки

- `describe`
- `health`
- `run collect`
- `run soft_shutdown_front`
- `run inspect_autorun`
- `run ensure_autorun`
- `run read_front_config`

## Порядок разработки

Рекомендую такой порядок, чтобы быстрее получить рабочий вертикальный срез:

1. Обновить контракт и маршрутизацию `task_type`.
2. Добавить общий reader полного `config.xml`.
3. Добавить parser `CRMid` и поиск `cash-server.log`.
4. Расширить `collect`, чтобы он уже возвращал `crm_id`.
5. Добавить discovery front installation и каталога `Plugins`.
6. Реализовать локальный scanner плагинов.
7. Добавить `soft_shutdown_front`.
8. Добавить `inspect_autorun`.
9. Добавить `ensure_autorun`.
10. Обновить документацию и тесты.

## Изменения в документации

Нужно обновить:

- `agent/docs/iiko_syrve_rms_adapter.md`
- при необходимости `agent/docs/ADAPTER_CONTRACT.md`, если список capabilities или примеры payload/result поменяются заметно

В документации следует явно зафиксировать:

- новые `task_type`;
- структуру `result`;
- поведение статусов `success/partial/failed`;
- структуру результата `inspect_autorun`, `ensure_autorun` и `read_front_config`;
- текущие ограничения для `Syrve`.

## Риски и точки внимания

- Путь к `cash-server.log` может отличаться между инсталляциями iiko, поэтому discovery должен быть не по одному hardcoded пути, а по набору кандидатов.
- Для локального инвентаря плагинов понадобится отдельный scanner внутри адаптера; простой список каталогов недостаточен, потому что нужно ещё определить DLL, API-версию и file version.
- У `WM_CLOSE` нет гарантии, что фронт успеет завершиться до освобождения портов; позже может понадобиться отдельный wait/poll слой.
- Версии плагинов могут отсутствовать в явном виде в имени каталога, поэтому без чтения version resource DLL результат будет неполным.
- Для ярлыков Startup в `goSSS` пока нет готового resolver-а `.lnk`, поэтому потребуется отдельная реализация чтения target path и arguments.
- Для инвентаризации планировщика текущего точечного поиска по exe недостаточно; придётся делать отдельный scanner задач и их actions.
- Полный снимок `config.xml` может быть объёмным, поэтому нужно заранее договориться о лимитах ответа и о том, нужен ли raw XML в `details`.
- Для `Syrve` почти наверняка понадобятся отдельные пути логов, имя процесса и, возможно, другой формат строк в логе.
- Документ и реализация не должны зависеть на наличие дополнительных внутренних проектов; все функции должны жить внутри `goSSS` и быть описаны здесь же.

## Открытые вопросы перед кодированием

Эти пункты не мешают сделать план, но их стоит подтвердить до реализации:

- Нужно ли в `collect` возвращать только данные, или ещё и признак, что фронт сейчас запущен.
- Как сервер хочет хранить список плагинов: как массив объектов в `result` адаптера или позже раскладывать их отдельно на backend.
- Нужен ли после `WM_CLOSE` короткий wait-loop на завершение процесса, или пока достаточно только отправки сообщения окну.
- Какой точный набор путей для `cash-server.log` считать приоритетным на боевых точках iiko.
- Нужно ли в `read_front_config` возвращать только плоский список настроек, или одновременно ещё и иерархическое дерево.
- Какое имя задачи планировщика считать стандартным для `ensure_autorun`.
- Должен ли `ensure_autorun` уметь не только создавать, но и заменять уже найденный другой способ автозапуска.
