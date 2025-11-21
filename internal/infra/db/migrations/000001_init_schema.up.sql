-- Up migration
CREATE TABLE companies (
  id TEXT PRIMARY KEY,
  meta_class TEXT DEFAULT 'ou$company',
  address TEXT,
  uuid TEXT UNIQUE,
  title TEXT,
  active_contract BOOLEAN,
  last_modified_date TIMESTAMPTZ,
  additional_name TEXT,
  parent_uuid TEXT
);
-- Внешний ключ для parent_uuid добавляем отдельно, чтобы избежать проблем с циклическими зависимостями при создании
-- ALTER TABLE companies ADD CONSTRAINT fk_companies_parent FOREIGN KEY (parent_uuid) REFERENCES companies(uuid);
-- Примечание: GORM может управлять отношениями без явного FK на уровне БД, но для целостности лучше его иметь.
-- Для упрощения и избежания проблем с порядком вставки компаний (родитель должен существовать) FK пока не добавляем,
-- целостность будет поддерживаться на уровне приложения.

CREATE INDEX idx_companies_uuid ON companies(uuid);
CREATE INDEX idx_companies_last_modified ON companies(last_modified_date);

CREATE TABLE servers (
  id TEXT PRIMARY KEY,
  meta_class TEXT DEFAULT 'objectBase$Server',
  unique_id TEXT,
  teamviewer TEXT,
  rdp TEXT,
  anydesk TEXT,
  uuid TEXT UNIQUE,
  ip TEXT,
  cabinet_link TEXT,
  device_name TEXT,
  last_modified_date TIMESTAMPTZ,
  litemanager TEXT,
  iiko_version TEXT,
  description TEXT,
  owner_id TEXT REFERENCES companies(uuid) ON DELETE SET NULL
);

CREATE INDEX idx_servers_uuid ON servers(uuid);
CREATE INDEX idx_servers_owner ON servers(owner_id);
CREATE INDEX idx_servers_last_modified ON servers(last_modified_date);

CREATE TABLE workstations (
  id TEXT PRIMARY KEY,
  meta_class TEXT DEFAULT 'objectBase$Workstation',
  teamviewer TEXT,
  anydesk TEXT,
  litemanager TEXT,
  device_name TEXT,
  last_modified_date TIMESTAMPTZ,
  description TEXT,
  uuid TEXT UNIQUE,
  owner_id TEXT REFERENCES companies(uuid) ON DELETE SET NULL
);

CREATE INDEX idx_workstations_uuid ON workstations(uuid);
CREATE INDEX idx_workstations_owner ON workstations(owner_id);
CREATE INDEX idx_workstations_last_modified ON workstations(last_modified_date);

CREATE TABLE fiscal_registers (
  id TEXT PRIMARY KEY,
  meta_class TEXT DEFAULT 'objectBase$FR',
  uuid TEXT UNIQUE,
  model_kkt TEXT,
  ffd TEXT,
  fr_downloader TEXT,
  rn_kkt TEXT,
  legal_name TEXT,
  fr_serial_number TEXT,
  fn_number TEXT,
  kkt_reg_date TIMESTAMPTZ,
  fn_expire_date TIMESTAMPTZ,
  last_modified_date TIMESTAMPTZ,
  owner_id TEXT REFERENCES companies(uuid) ON DELETE SET NULL,
  address TEXT,
  attribute_excise BOOLEAN,
  attribute_marked BOOLEAN,
  ofd_name TEXT
);

CREATE INDEX idx_fr_uuid ON fiscal_registers(uuid);
CREATE INDEX idx_fr_owner ON fiscal_registers(owner_id);
CREATE INDEX idx_fr_last_modified ON fiscal_registers(last_modified_date);