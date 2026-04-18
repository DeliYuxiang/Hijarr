# Hijarr - Torznab Proxy & Metadata MITM for Chinese Anime Indexers

Hijarr is a specialized proxy middleware designed to bridge the gap between English-centric media managers (like Sonarr/Bazarr) and Chinese anime indexers (like DMHY/Assrt). It ensures that Sonarr can find Chinese-titled releases by translating titles via TMDB and mapping seasons to Chinese conventions.

## 🏗️ Architecture & Core Logic

- **Torznab Proxy (`/api`):** Acts as a middleware between Sonarr/Prowlarr and indexers. It intercepts search queries, translates them to Chinese using the TMDB API, and adjusts parameters (like season/episode numbers) to match common Chinese release naming patterns.
- **TVDB & Skyhook MITM Proxy (`/*`):** Transparently hijacks requests to `api.thetvdb.com` and `skyhook.sonarr.tv`. It can:
    - **Language Switching:** Intercept outgoing requests and replace requested languages (e.g., `eng`) with a target language (e.g., `zho`).
    - **Metadata Overwriting:** Fetch series and episode names from TMDB in a target language (e.g., `zh-CN`) and inject them into the JSON response, ensuring high-quality translations for Sonarr.
- **Assrt Hijacking Proxy:** Intercepts requests to `api.assrt.net` to provide "Fission Search". It translates English queries to Chinese and performs multiple concurrent searches with various naming conventions (e.g., "Season X", "Episode Y", sub-titles) to maximize subtitle hit rates for Bazarr.
- **TMDB Translation:** Uses the TMDB API to fetch series and episode titles.
- **Season Mapping:** Features a manual mapping system (`AnimeSeasonMap`) in `internal/config/config.go` to handle complex cases.

## 🛠️ Technology Stack

- **Language:** Go 1.22+
- **Framework:** [Gin](https://gin-gonic.com/)
- **Dependency Management:** Go Modules
- **Containerization:** Docker (Multi-stage build using `scratch`)

## 🚀 Building and Running

### Local Development
1. Install dependencies:
   ```bash
   go mod download
   ```
2. Run the application:
   ```bash
   go run cmd/Hijarr/main.go
   ```

### Docker
1. Build using the Go-specific Dockerfile:
   ```bash
   docker build -t Hijarr -f Dockerfile.golang .
   ```

## ⚙️ Configuration (Environment Variables)

- `PROWLARR_TARGET_URL`: The API endpoint of your Prowlarr instance.
- `PROWLARR_API_KEY`: Your Prowlarr API key.
- `TMDB_API_KEY`: Required for metadata translation.
- `TARGET_LANGUAGE`: Language for TMDB lookups (default: `zh-CN`).
- `TVDB_LANGUAGE`: Language for TVDB internal requests (default: `zho`).

## 📝 Development Conventions

- **Code Style:** Standard Go formatting (`go fmt`).
- **Core Logic Location:** `internal/proxy/` contains the main proxying logic for Torznab, TVDB, and Assrt.
- **Manual Overrides:** Add custom season mappings to the `AnimeSeasonMap` dictionary in `internal/config/config.go`.
