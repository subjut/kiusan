# Installation

Kiusan is a single static binary with everything embedded. The easiest ways to run it are Docker or building from source and running it directly. Either way, all of its data lives in one **data dir** (default: the directory it is run from), wich contains the `kiusan.yaml` config file and the database file, if the default SQLite backend is used.

## Contents

- [Docker](#docker)
- [Manual](#manual)
- [Configuration](#configuration)
- [Choosing a database backend](#choosing-a-database-backend)


## Docker

The image is based on `alpine` and is published as `ghcr.io/subjut/kiusan` for amd64 and arm64, tagged with the version (e.g. `v0.5.0`) and `latest`. The versioned tags make it easy to roll back to a known-good release if a newer image breaks something.


### Permissions & where the data lives

Inside the container, Kiusan runs as the non-root user `kiusan` (uid `1000`) and expects the data dir in `/data`.

- **Named volume** (see examples below): Docker sets the ownership for you, nothing to do.
- **Bind mount** — if you'd rather keep the data in a directory you choose (a compose `./data:/data` volume entry or a `docker run` `-v` flag), the host directory has to be owned by the container's user, or Kiusan cannot write to it:

  ```sh
  chown -R 1000:1000 ./data
  ```

To back up an installation with the default SQLite backend, copy the contents of the data dir; with an external database, back up the database itself and the `kiusan.yaml` config file in the data dir.


### Docker Compose

A minimal setup with the default SQLite backend looks like this:

```yaml
services:
  kiusan:
    image: ghcr.io/subjut/kiusan:latest
    ports:
      - "8080:8080"
    environment:
      KIUSAN_BASE_URL: https://your.domain
    volumes:
      - data:/data
    restart: unless-stopped

volumes:
  data:
```

`docker compose up -d`, then open <http://localhost:8080>.

Set `KIUSAN_BASE_URL` to the domain you serve Kiusan on. It is used to construct the links for the meetings and voting polls. A value starting with `https://` additionally marks the response cookies as `Secure`.

The repository ships a ready-to-use [`docker-compose.yml`](docker-compose.yml) that uses the above setup with additional comments for Postgres.

If you wish to use another database backend, you need to add another service and set the appropirate [environment variables](#configuration). Here is an example for **Postgres**:

```yaml
services:
  kiusan:
    image: ghcr.io/subjut/kiusan:latest
    ports:
      - "8080:8080"
    environment:
      KIUSAN_BASE_URL: https://your.domain
      KIUSAN_STORE: postgres
      KIUSAN_DB_HOST: db
      KIUSAN_DB_USER: kiusan
      KIUSAN_DB_PASSWORD: secret
      KIUSAN_DB_NAME: kiusan
      KIUSAN_DB_SSLMODE: disable
    volumes:
      - data:/data
    restart: unless-stopped
    depends_on:
      - db

  db:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: kiusan
      POSTGRES_PASSWORD: secret
      POSTGRES_DB: kiusan
    volumes:
      - pgdata:/var/lib/postgresql/data

volumes:
  data:
  pgdata:
```

If you use an already running database backend, be aware that you need to create the database referenced by `KIUSAN_DB_NAME` manually before starting Kiusan. It only creates the tables, not the database itself.

For MySQL or MariaDB, set `KIUSAN_STORE: mysql` or `mariadb` and adjust the db section:

```yaml
  db:
    image: mysql:8
    environment:
      MYSQL_USER: kiusan
      MYSQL_PASSWORD: secret
      MYSQL_DATABASE: kiusan
    volumes:
      - mydata:/var/lib/mysql
```

For MariaDB use the image `mariadb:11` and the `MARIADB_*` equivalents of the
environment variables.

If you use a non-standard db port also set `KIUSAN_DB_PORT`.


### Plain Docker

The same setup as a `docker run`:

```sh
docker run -d \
  --name kiusan \
  --restart unless-stopped \
  -p 8080:8080 \
  -e KIUSAN_BASE_URL=https://your.domain \
  -v kiusan-data:/data \
  ghcr.io/subjut/kiusan:latest
```

Set `KIUSAN_BASE_URL` to the domain you serve Kiusan on. It is used to construct the links for the meetings and voting polls. A value starting with `https://` additionally marks the response cookies as `Secure`.


## Manual

Building Kiusan needs Go 1.26+:

```sh
go build -o kiusan .
./kiusan
```

On first run it creates the config file `kiusan.yaml` and the SQLite database file `meetings.db` in the same directory as the `kiusan` binary itself. To separate the binary from the data dir, point `KIUSAN_DATA_DIR` somewhere stable:

```sh
KIUSAN_DATA_DIR=/var/lib/kiusan ./kiusan
```

Run it under whatever service manager you like; it listens on `:8080` (configurable, see below).


### Running it as a systemd service

It's recommended to give Kiusan its own user and keep the data dir apart from the binary:

```sh
useradd --system kiusan
mkdir -p /var/lib/kiusan
chown kiusan:kiusan /var/lib/kiusan
```

`/etc/systemd/system/kiusan.service`:

```ini
[Unit]
Description=Kiusan
After=network.target

[Service]
User=kiusan
Environment=KIUSAN_DATA_DIR=/var/lib/kiusan
ExecStart=/usr/local/bin/kiusan
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

Then `systemctl daemon-reload` and `systemctl enable --now kiusan`.


## Configuration

Settings have the following precedence with 1. being the highest:

1. the environment variables `KIUSAN_*`,
2. `kiusan.yaml` in the data dir,
3. the built-in default.

Values provided by the environment are also **written into the settings file** on every start, so removing an environment variable does not undo the setting. Additionally, you need to edit `kiusan.yaml` or set the environment variable to something new once.

The file is created on first run (mode `0600`) and carries the auto-generated CSRF secret, so a fresh install works without configuring a single key.

| Variable | File key | Default | What it does |
| --- | --- | --- | --- |
| `KIUSAN_PORT` | `port` | `8080` | The TCP port the app listens on. |
| `KIUSAN_BASE_URL` | `base_url` | *(empty — the request's scheme + host)* | The public URL of the installation, needed for poll links. A value starting with `https://` additionally marks the response cookies as `Secure`. |
| `KIUSAN_SECRET` | `secret` | *(generated once on first run)* | The CSRF signing secret. Should be persistent across restarts. |
| `KIUSAN_DEV` | `dev` | off | Development mode: template changes are picked up without a restart, template parse errors are logged. |
| `KIUSAN_STORE` | `store` | `sqlite` | The storage backend: `sqlite`, `mysql`, `mariadb`, or `postgres`. |
| `KIUSAN_DB_HOST` | `db.host` | `127.0.0.1` | The database host. IP or hostname. Required for MySQL/MariaDB/Postgres. |
| `KIUSAN_DB_PORT` | `db.port` | `3306` (mysql/mariadb), `5432` (postgres) | The database port. |
| `KIUSAN_DB_USER` | `db.user` | *(required for MySQL/MariaDB/Postgres)* | The database user. |
| `KIUSAN_DB_PASSWORD` | `db.password` | *(empty)* | The database password. |
| `KIUSAN_DB_NAME` | `db.name` | `meetings.db` | sqlite: the data file's name in the data dir. MySQL/MariaDB/Postgres: the database name (the database itself must already exist; Kiusan auto-creates the tables). |
| `KIUSAN_DB_SSLMODE` | `db.sslmode` | `disable` | The postgres SSL mode (postgres only). |
| `KIUSAN_DATA_DIR` | *(env only)* | `.` | The data dir for the config file and the sqlite file. |


## Choosing a database backend

- **SQLite** (default): embedded, using a single file and doesn't require a separate service, so is lighter on resources. For self hosted installations with only a few users that's fine.
- **PostgreSQL**: if you expect heavy usage with lots of concurrent polls, this is the better choice.
- **MySQL / MariaDB**: the same as PostgreSQL.
