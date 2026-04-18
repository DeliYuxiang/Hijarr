FROM node:22-alpine
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN ls -la
RUN npm install
