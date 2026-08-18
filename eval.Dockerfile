# Evaluation image: full Go toolchain + Node.js so both backend and frontend
# can be built offline inside the container.
FROM golang:1.26

WORKDIR /app

ENV GOTOOLCHAIN=local

# Install curl, then Node.js via the NodeSource setup script.
RUN apt-get update && apt-get install -y --no-install-recommends curl ca-certificates && \
    rm -rf /var/lib/apt/lists/* && \
    curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
    apt-get install -y --no-install-recommends nodejs && \
    rm -rf /var/lib/apt/lists/*

# Download Go module dependencies first so this layer is cached.
COPY go.mod go.sum ./
RUN go mod download

# Install frontend dependencies (cached separately from source).
COPY web/package.json web/package-lock.json ./web/
RUN cd web && npm install

# Copy the rest of the source and build both frontend and backend.
COPY . .
RUN cd web && npm run build
RUN go build ./...

CMD ["bash"]
