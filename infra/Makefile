.PHONY: deploy logs local-deploy down

deploy:
	git pull --ff-only
	docker compose --env-file infra/.env.production -f infra/compose.prod.yml up -d --build bistbot

logs:
	docker compose --env-file infra/.env.production -f infra/compose.prod.yml logs -f bistbot

local-deploy:
	docker compose --env-file infra/.env.production.example -f infra/compose.prod.yml up -d --build bistbot

down:
	docker compose --env-file infra/.env.production -f infra/compose.prod.yml down
