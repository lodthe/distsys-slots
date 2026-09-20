# HTTPS через Nginx

Приложению и Telegram Mini App нужен публичный HTTPS-домен.
[slots.conf.example](slots.conf.example) рассчитан на Nginx на том же хосте,
что и Docker Compose: backend доступен только на `127.0.0.1:8083`.

1. Направьте DNS своего домена на сервер и получите TLS-сертификат.
2. Скопируйте пример в конфигурацию Nginx, замените `slots.example.com`
   своим доменом и укажите пути к сертификату и ключу.
3. Задайте тот же HTTPS origin в `APP_BASE_URL` без пути и query string.
4. Выполните `sudo nginx -t`, затем `sudo systemctl reload nginx`.
5. Проверьте `https://ВАШ-ДОМЕН/readyz`, затем выполните
   `docker compose exec app distsys configure-bot`.

Nginx перезаписывает forwarded-заголовки; приложение использует `X-Real-IP`
для ограничения частоты входа. Если перед Nginx стоит CDN или другой прокси,
настройте определение реального IP только для доверенных адресов этого прокси
и TLS на участке между ним и сервером.

Сертификаты и закрытые ключи хранятся вне репозитория.
