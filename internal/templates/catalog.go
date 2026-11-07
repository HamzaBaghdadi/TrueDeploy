package templates

// Images are pinned to a major version (or the vendor's stable/LTS channel
// where no major tag exists), so redeploys pick up fixes but never a
// breaking release. Data lives in named volumes, which survive redeploys
// and stopping the app.
var catalog = []Template{
	{
		ID:          "uptime-kuma",
		Name:        "Uptime Kuma",
		Tagline:     "Monitor websites and services, with status pages and alerts.",
		Category:    "Monitoring",
		Website:     "https://uptime.kuma.pet",
		Image:       "louislam/uptime-kuma:2",
		DefaultPort: 3001,
		Notes: []string{
			"Open the app and create the admin account on the first visit. Do this right away: until then, anyone who opens it can.",
		},
		Compose: `services:
  app:
    image: louislam/uptime-kuma:2
    restart: unless-stopped
    ports:
      - "${BIND}:${PORT}:3001"
    volumes:
      - data:/app/data
volumes:
  data:
`,
	},
	{
		ID:          "n8n",
		Name:        "n8n",
		Tagline:     "Workflow automation: connect APIs and apps with a visual editor.",
		Category:    "Automation",
		Website:     "https://n8n.io",
		Image:       "n8nio/n8n:stable",
		DefaultPort: 5678,
		WantsDomain: true,
		Vars: []Var{
			{Key: "N8N_ENCRYPTION_KEY", Label: "Encryption key", Secret: true, Length: 40, Hint: "Encrypts saved credentials. Keep it: changing it makes them unreadable."},
			{Key: "TZ", Label: "Time zone", Default: "UTC", Hint: "Used by schedules, e.g. Europe/Berlin."},
		},
		Notes: []string{
			"Open the app and create the owner account on the first visit.",
			"Webhooks use the project's first domain, so add one before building workflows that receive webhooks.",
		},
		Compose: `services:
  app:
    image: n8nio/n8n:stable
    restart: unless-stopped
    ports:
      - "${BIND}:${PORT}:5678"
    environment:
      N8N_HOST: ${PUBLIC_HOST}
      N8N_EDITOR_BASE_URL: ${PUBLIC_URL}/
      WEBHOOK_URL: ${PUBLIC_URL}/
      N8N_PROXY_HOPS: "1"
      N8N_SECURE_COOKIE: ${HTTPS}
      N8N_ENCRYPTION_KEY: ${N8N_ENCRYPTION_KEY}
      GENERIC_TIMEZONE: ${TZ}
      TZ: ${TZ}
    volumes:
      - data:/home/node/.n8n
volumes:
  data:
`,
	},
	{
		ID:          "wordpress",
		Name:        "WordPress",
		Tagline:     "The website and blog platform, with its own MariaDB database.",
		Category:    "Websites",
		Website:     "https://wordpress.org",
		Image:       "wordpress:7-apache",
		DefaultPort: 8080,
		WantsDomain: true,
		Vars: []Var{
			{Key: "DB_PASSWORD", Label: "Database password", Secret: true},
			{Key: "DB_ROOT_PASSWORD", Label: "Database root password", Secret: true},
		},
		Notes: []string{
			"Open the app to run the WordPress installer: pick the site title and admin account.",
			"Add the domain before installing: WordPress remembers the address it was installed at.",
		},
		Compose: `services:
  wordpress:
    image: wordpress:7-apache
    restart: unless-stopped
    depends_on: [db]
    ports:
      - "${BIND}:${PORT}:80"
    environment:
      WORDPRESS_DB_HOST: db
      WORDPRESS_DB_USER: wordpress
      WORDPRESS_DB_PASSWORD: ${DB_PASSWORD}
      WORDPRESS_DB_NAME: wordpress
    volumes:
      - site:/var/www/html
  db:
    image: mariadb:lts
    restart: unless-stopped
    environment:
      MARIADB_DATABASE: wordpress
      MARIADB_USER: wordpress
      MARIADB_PASSWORD: ${DB_PASSWORD}
      MARIADB_ROOT_PASSWORD: ${DB_ROOT_PASSWORD}
    volumes:
      - db:/var/lib/mysql
volumes:
  site:
  db:
`,
	},
	{
		ID:          "ghost",
		Name:        "Ghost",
		Tagline:     "Publishing platform for blogs and newsletters, with MySQL.",
		Category:    "Websites",
		Website:     "https://ghost.org",
		Image:       "ghost:6",
		DefaultPort: 2368,
		WantsDomain: true,
		Vars: []Var{
			{Key: "DB_PASSWORD", Label: "Database password", Secret: true},
		},
		Notes: []string{
			"Open <your address>/ghost to create the admin account.",
			"Ghost builds every link from the project's first domain, so add the domain first.",
		},
		Compose: `services:
  ghost:
    image: ghost:6
    restart: unless-stopped
    depends_on: [db]
    ports:
      - "${BIND}:${PORT}:2368"
    environment:
      url: ${PUBLIC_URL}
      database__client: mysql
      database__connection__host: db
      database__connection__user: root
      database__connection__password: ${DB_PASSWORD}
      database__connection__database: ghost
    volumes:
      - content:/var/lib/ghost/content
  db:
    image: mysql:8.4
    restart: unless-stopped
    environment:
      MYSQL_ROOT_PASSWORD: ${DB_PASSWORD}
      MYSQL_DATABASE: ghost
    volumes:
      - db:/var/lib/mysql
volumes:
  content:
  db:
`,
	},
	{
		ID:          "gitea",
		Name:        "Gitea",
		Tagline:     "Lightweight Git hosting with issues, pull requests and CI.",
		Category:    "Developer tools",
		Website:     "https://about.gitea.com",
		Image:       "gitea/gitea:1",
		DefaultPort: 3000,
		Notes: []string{
			"Open the app to finish the installer; the first account you register becomes the admin.",
			"Clone over HTTPS. SSH cloning isn't published by this template.",
		},
		Compose: `services:
  app:
    image: gitea/gitea:1
    restart: unless-stopped
    ports:
      - "${BIND}:${PORT}:3000"
    environment:
      USER_UID: "1000"
      USER_GID: "1000"
      GITEA__server__ROOT_URL: ${PUBLIC_URL}/
      GITEA__server__DOMAIN: ${PUBLIC_HOST}
      GITEA__server__DISABLE_SSH: "true"
    volumes:
      - data:/data
volumes:
  data:
`,
	},
	{
		ID:          "vaultwarden",
		Name:        "Vaultwarden",
		Tagline:     "Password manager server that works with the Bitwarden apps.",
		Category:    "Security",
		Website:     "https://github.com/dani-garcia/vaultwarden",
		Image:       "vaultwarden/server:latest",
		DefaultPort: 8222,
		WantsDomain: true,
		Vars: []Var{
			{Key: "ADMIN_TOKEN", Label: "Admin panel token", Secret: true, Length: 48, Hint: "Opens <your address>/admin."},
			{Key: "SIGNUPS_ALLOWED", Label: "Allow sign-ups", Default: "true", Hint: "Set to false once your accounts exist."},
		},
		Notes: []string{
			"Needs a domain: browsers only allow the web vault over HTTPS.",
			"Create your account, then set SIGNUPS_ALLOWED to false and redeploy.",
		},
		Compose: `services:
  app:
    image: vaultwarden/server:latest
    restart: unless-stopped
    ports:
      - "${BIND}:${PORT}:80"
    environment:
      DOMAIN: ${PUBLIC_URL}
      ADMIN_TOKEN: ${ADMIN_TOKEN}
      SIGNUPS_ALLOWED: ${SIGNUPS_ALLOWED}
    volumes:
      - data:/data
volumes:
  data:
`,
	},
	{
		ID:          "metabase",
		Name:        "Metabase",
		Tagline:     "Dashboards and questions on top of your databases.",
		Category:    "Analytics",
		Website:     "https://www.metabase.com",
		Image:       "metabase/metabase:latest",
		DefaultPort: 3030,
		DBNetwork:   true,
		Vars: []Var{
			{Key: "MB_DB_PASSWORD", Label: "Internal database password", Secret: true},
		},
		Notes: []string{
			"Metabase takes a minute or two to start the first time.",
			"To chart a TrueDeploy database, add it in Metabase with host td-db-<name> (shown on the database's page).",
		},
		Compose: `services:
  app:
    image: metabase/metabase:latest
    restart: unless-stopped
    depends_on: [db]
    ports:
      - "${BIND}:${PORT}:3000"
    environment:
      MB_SITE_URL: ${PUBLIC_URL}
      MB_DB_TYPE: postgres
      MB_DB_HOST: db
      MB_DB_PORT: "5432"
      MB_DB_DBNAME: metabase
      MB_DB_USER: metabase
      MB_DB_PASS: ${MB_DB_PASSWORD}
  db:
    image: postgres:17
    restart: unless-stopped
    environment:
      POSTGRES_DB: metabase
      POSTGRES_USER: metabase
      POSTGRES_PASSWORD: ${MB_DB_PASSWORD}
    volumes:
      - db:/var/lib/postgresql/data
volumes:
  db:
`,
	},
	{
		ID:          "portainer",
		Name:        "Portainer",
		Tagline:     "Web UI for the server's Docker: containers, images, volumes.",
		Category:    "Developer tools",
		Website:     "https://www.portainer.io",
		Image:       "portainer/portainer-ce:lts",
		DefaultPort: 9000,
		Notes: []string{
			"Create the admin account within 5 minutes of the first start, or Portainer locks itself; redeploy to unlock it.",
			"Portainer controls Docker on this server, which is close to full control of the server. Protect it with a strong password.",
		},
		Compose: `services:
  app:
    image: portainer/portainer-ce:lts
    restart: unless-stopped
    ports:
      - "${BIND}:${PORT}:9000"
    volumes:
      - ${DOCKER_SOCK:-/var/run/docker.sock}:/var/run/docker.sock
      - data:/data
volumes:
  data:
`,
	},
	{
		ID:          "seaweedfs",
		Name:        "S3 storage (SeaweedFS)",
		Tagline:     "S3-compatible storage for images and uploads, used with any S3 SDK.",
		Category:    "Storage",
		Website:     "https://github.com/seaweedfs/seaweedfs",
		Image:       "chrislusf/seaweedfs:latest",
		DefaultPort: 8333,
		WantsDomain: true,
		Vars: []Var{
			{Key: "S3_ACCESS_KEY", Label: "Access key", Secret: true, Length: 20},
			{Key: "S3_SECRET_KEY", Label: "Secret key", Secret: true, Length: 40},
			{Key: "PUBLIC_READ", Label: "Public read access", Default: "false",
				Hint: "true lets anyone download files by URL (for images on a website); uploads always need the keys."},
		},
		Notes: []string{
			"Endpoint: the app's address (its domain, e.g. https://files.example.com). Region: us-east-1. Turn on path-style addressing (forcePathStyle: true in the AWS SDK).",
			"The access and secret keys are in the app's environment variables (Edit). Put them in your backend's environment, then create a bucket from the backend or with the AWS CLI.",
			"With public read on, files are served at <endpoint>/<bucket>/<file>.",
		},
		Compose: `services:
  s3:
    image: chrislusf/seaweedfs:latest
    restart: unless-stopped
    ports:
      - "${BIND}:${PORT}:8333"
    environment:
      S3_ACCESS_KEY: ${S3_ACCESS_KEY}
      S3_SECRET_KEY: ${S3_SECRET_KEY}
      PUBLIC_READ: ${PUBLIC_READ}
    # Writes the S3 identities from the environment, then starts the server.
    entrypoint: ["/bin/sh", "-c"]
    command:
      - |
        anon=""
        [ "$$PUBLIC_READ" = "true" ] && anon=',{"name":"anonymous","actions":["Read"]}'
        printf '{"identities":[{"name":"admin","credentials":[{"accessKey":"%s","secretKey":"%s"}],"actions":["Admin","Read","List","Tagging","Write"]}%s]}' \
          "$$S3_ACCESS_KEY" "$$S3_SECRET_KEY" "$$anon" > /tmp/s3.json
        exec weed server -dir=/data -s3 -s3.port=8333 -s3.config=/tmp/s3.json -master.volumeSizeLimitMB=1024
    volumes:
      - data:/data
volumes:
  data:
`,
	},
	{
		ID:          "adminer",
		Name:        "Adminer",
		Tagline:     "Browse and edit your TrueDeploy databases in the browser.",
		Category:    "Databases",
		Website:     "https://www.adminer.org",
		Image:       "adminer:5",
		DefaultPort: 8081,
		DBNetwork:   true,
		Notes: []string{
			"Sign in with the server td-db-<name> and the user and password from the database's page.",
			"Adminer gives access to your data: use a domain (HTTPS) or stop it when you're done.",
		},
		Compose: `services:
  app:
    image: adminer:5
    restart: unless-stopped
    ports:
      - "${BIND}:${PORT}:8080"
    environment:
      ADMINER_DESIGN: dracula
`,
	},
}
