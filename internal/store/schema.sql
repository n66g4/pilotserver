CREATE TABLE IF NOT EXISTS devices (
  dongle_id TEXT PRIMARY KEY,
  public_key_pem TEXT NOT NULL,
  alias TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
