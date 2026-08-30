## Bulk Mail

[![CI](https://github.com/dvoulgaridis/bulk-mail/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/dvoulgaridis/bulk-mail/actions/workflows/ci.yml)

Bulk Mail enables mail merging with personalized attachments and delivery. It imports address lists, renders personalized email and DOCX templates, converts documents to PDF and sends them through a custom SMTP profile or Gmail account.

### Requirements

- Go 1.26+
- Node.js 22.13+
- pnpm 11.22
- LibreOffice

### Quick Start

```sh
git clone https://github.com/dvoulgaridis/bulk-mail.git
cd bulk-mail
pnpm install --frozen-lockfile
pnpm run build:ui
go run ./cmd/bulk-mail
```

For frontend development, run the Go server and Vite dev server separately:

Terminal 1 — Go backend:

```sh
go run ./cmd/bulk-mail --no-browser --port 18765
```

Terminal 2 — Vite frontend:

```sh
pnpm run dev:ui
```

### Build

```sh
pnpm install --frozen-lockfile
go run ./scripts/build.go --version v0.1.0
```

The builder creates versioned platform archives and `checksums.txt` under `dist/`.

### Run

Linux:

```sh
tar -xzf ./dist/bulk-mail-v0.1.0-linux-amd64.tar.gz
cd bulk-mail
./bulk-mail
```

macOS:

```sh
tar -xzf ./dist/bulk-mail-v0.1.0-darwin-arm64.tar.gz
cd bulk-mail
./bulk-mail
```

Windows PowerShell:

```powershell
Expand-Archive .\dist\bulk-mail-v0.1.0-windows-amd64.zip -DestinationPath .
Set-Location .\bulk-mail
.\bulk-mail.exe
```

### Supported Personalized Documents

| Format | Extensions |
| --- | --- |
| Microsoft Word Open XML Document | `.docx` |

### Supported Address Lists

| Format | Extensions | Import | Export |
| --- | --- | --- | --- |
| Comma-separated values | `.csv` | Yes | Yes |
| Tab-separated values | `.tsv` | Yes | No |
| Excel workbook | `.xlsx` | Yes | No |
| vCard | `.vcf`, `.vcard` | Yes | Yes |
