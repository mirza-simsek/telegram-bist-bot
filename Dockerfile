# ---- build stage ----
FROM golang:1.23-alpine AS go-build
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY cmd cmd
COPY internal internal
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w" -o /build/bistbot ./cmd/bistbot

# ---- final stage ----
FROM python:3.12-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata procps \
    && rm -rf /var/lib/apt/lists/*
ENV TZ=Europe/Istanbul

RUN useradd --system --create-home --home-dir /app --shell /usr/sbin/nologin bistbot
WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY --from=go-build /build/bistbot /app/bistbot
COPY scripts ./scripts
COPY data ./data

ENV PYTHON_EXECUTABLE=python3 \
    PYTHON_SCANNER_SCRIPT=scripts/bist_data_scrap_bridge.py \
    ALL_SYMBOLS_FILE=data/bist_tum_hisseler.txt \
    BIST100_SYMBOLS_FILE=data/bist_100_hisseler.txt

RUN chown -R bistbot:bistbot /app
USER bistbot
ENTRYPOINT ["/app/bistbot"]
