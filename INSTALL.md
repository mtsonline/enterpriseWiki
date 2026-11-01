# Installationsanleitung Enterprise Wiki v6.0.0

```bash
tar -xzf enterprise-wiki-v6.0.0-production.tar.gz
cd enterprise-wiki
docker-compose down -v
docker-compose up -d --build
docker exec -it wiki-backend ./server --setup
```
