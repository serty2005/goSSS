# Stage 1: Build
FROM node:24-alpine as builder
WORKDIR /app

# Сначала копируем package файлы для кэширования слоев
COPY package*.json ./
RUN npm ci

# Копируем исходники и собираем
COPY . .
# Важно: TypeScript проверка может быть строгой, если будут ошибки сборки - 
# можно временно использовать "npm run build -- --emptyOutDir" или отключить tsc в package.json
RUN npm run build

# Stage 2: Serve
FROM nginx:alpine
# Копируем кастомный конфиг
COPY nginx.conf /etc/nginx/conf.d/default.conf
# Копируем собранные файлы из предыдущего этапа
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]