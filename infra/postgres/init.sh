#!/bin/sh
set -eu

# The official entrypoint runs this once when the PostgreSQL volume is empty.
# psql variables quote password values as SQL literals without printing them.
psql --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
  --set=ON_ERROR_STOP=1 \
  --set=koujou_password="$KOUJOU_DB_PASSWORD" \
  --set=kratos_password="$KRATOS_DB_PASSWORD" \
  --set=hydra_password="$HYDRA_DB_PASSWORD" <<'SQL'
CREATE ROLE koujou LOGIN PASSWORD :'koujou_password';
CREATE ROLE kratos LOGIN PASSWORD :'kratos_password';
CREATE ROLE hydra LOGIN PASSWORD :'hydra_password';
CREATE DATABASE koujou OWNER koujou;
CREATE DATABASE kratos OWNER kratos;
CREATE DATABASE hydra OWNER hydra;
REVOKE ALL ON DATABASE koujou FROM PUBLIC;
REVOKE ALL ON DATABASE kratos FROM PUBLIC;
REVOKE ALL ON DATABASE hydra FROM PUBLIC;
SQL
