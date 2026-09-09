# LexScriptsAI V3 — Production Backend

Court-grade, multi-tenant legal transcription backend built with **Go (Golang)**, **Echo v4**, **PostgreSQL (GORM)**, **Google Cloud Storage (GCS)**, and asynchronous **Whisper Speech-to-Text API** orchestration.

---

## 1. Architectural Highlights

- **Multi-Tenant Isolation**: Each Account Owner represents an independent tenant. Every database query enforces tenancy using `account_id` derived from the authenticated JWT session context (never trusting client input).
- **System Roles vs. Professional Roles**:
  - **System Roles**: `admin`, `owner`, `sub_account`
  - **Sub-Account Professional Roles**: `judge`, `court_reporter`, `scopist`
- **Permissions Engine**: The Admin manages users and professional roles, but does **NOT** dictate transcript permissions. The Account Owner controls member transcript permissions (`view`, `edit`, `upload`, `export`, `delete`, `restore`, `share`, `collaborate`, and specific transcript scoping).
- **Asynchronous Whisper Transcription**:
  - Target Service: `http://194.93.48.51:8070`
  - Token: `RlU7oyEnR9yuqGLBWdLk1vkwSx3U7d_bMlQPh3YIJjs`
  - Worker Pool: Non-blocking job queue with automatic polling, speaker diarization, word-level timestamp alignment, failure retries, and in-app completion notifications.
- **Secure File Storage**:
  - GCS integration with private buckets and short-lived (15-minute) signed PUT/GET URLs.
  - Strict MIME type and size limit validation (max 500MB).
- **Cause Lists & Adjournments**:
  - Date-based cause lists with court matters (`Hearing`, `Mention`, `Motion`, `Ruling`, `Judgment`).
  - Adjournment tracking with reason and automatic scheduling to new dates.
- **Recycle Bin**:
  - 7-day recoverable soft deletion with restore capability and authorized permanent purge.
- **Enterprise Security**:
  - Strict CORS whitelist, HSTS, CSP, X-Frame-Options (DENY), X-Content-Type-Options.
  - Password hashing with Bcrypt (cost 12+).
  - JWT token rotation with short-lived access tokens (15m) and secure refresh tokens (7d).
  - Comprehensive legal audit logging for compliance.

---

## 2. Quick Start

### Prerequisites
- Go 1.22+ installed
- PostgreSQL running locally or in the cloud (default: `localhost:5432`)

### 1. Configure Environment
Copy `.env.example` to `.env`:
```bash
cp .env.example .env
```

Default configuration:
```env
PORT=8080
DATABASE_URL=postgres://postgres:postgres@localhost:5432/lexscriptsai_v3?sslmode=disable
JWT_SECRET=lexscriptsai-v3-super-secret-production-grade-jwt-key-2026
WHISPER_API_URL=http://194.93.48.51:8070
WHISPER_BASE_TOKEN=RlU7oyEnR9yuqGLBWdLk1vkwSx3U7d_bMlQPh3YIJjs
GCS_BUCKET_NAME=exscripts-ai-media
GOOGLE_APPLICATION_CREDENTIALS=service-account.json
FRONTEND_URL=http://localhost:5173
```

### 2. Build & Run
```bash
# Build binary
go build -o bin/server.exe cmd/server/main.go

# Run server
./bin/server.exe
```
On startup, database extensions (`uuid-ossp`, `pgcrypto`), tables, initial Nigerian locations, default admin user, and sample demo accounts are automatically initialized and seeded.

---

## 3. Seed Accounts & Credentials

| Role | Email | Password | Scope |
|---|---|---|---|
| **System Admin** | `admin@lexscriptsai.com` | `AdminPassword2026!#` | System-wide platform management |
| **Account Owner** | `victor@lawfirm.ng` | `password123` | Firm tenant management, members, cases |
| **Court Reporter** | `samuel@lawfirm.ng` | `password123` | Tenant sub-account (Court Reporter) |
| **Scopist** | `esther@lawfirm.ng` | `password123` | Tenant sub-account (Scopist) |
| **Judge** | `david@lawfirm.ng` | `password123` | Tenant sub-account (Judge) |

---

## 4. API Endpoints Reference (`/api/v1`)

### Public & Auth
- `POST /api/v1/auth/login` — User & admin authentication
- `POST /api/v1/auth/refresh` — Refresh access token
- `GET /api/v1/auth/me` — Authenticated user profile (Requires Bearer token)

### System Admin (`RequireAdmin` role)
- `GET /api/v1/admin/stats` — Platform metrics (accounts, transcripts, users, hours processed)
- `GET /api/v1/admin/logs` — Platform audit and security trail
- `GET /api/v1/admin/accounts` — Paginated account directory
- `POST /api/v1/admin/accounts` — Provision Account Owner + tenant account
- `POST /api/v1/admin/accounts/:id/sub-accounts` — Add member (`judge`, `court_reporter`, or `scopist`)
- `PATCH /api/v1/admin/users/:id/status` — Update user status (`active`, `disabled`, `suspended`)
- `PATCH /api/v1/admin/accounts/:id/status` — Update account status
- `POST /api/v1/admin/locations` — Create court/firm location
- `PUT /api/v1/admin/locations/:id` — Update location details

### Account Owner & Member Permissions (`RequireAccountOwner` role)
- `GET /api/v1/accounts/members/:userId/permissions` — Inspect member permissions
- `PUT /api/v1/accounts/members/:userId/permissions` — Configure permissions and transcript scoping

### File & Storage Management
- `POST /api/v1/files/upload-url` — Generate pre-signed GCS upload URL
- `POST /api/v1/files/direct` — Multipart audio upload proxy
- `GET /api/v1/files/:id/signed-url` — Short-lived streaming/download URL

### Transcripts & Scoping Editor
- `GET /api/v1/transcripts` — List tenant transcripts with filtering & pagination
- `GET /api/v1/transcripts/:id` — Fetch transcript with speaker banks and timestamps
- `POST /api/v1/transcripts` — Create transcript / trigger transcription job
- `PATCH /api/v1/transcripts/:id` — Save scoping edits (speaker banks, word timestamps)
- `DELETE /api/v1/transcripts/:id` — Move transcript to recycle bin

### Folders & Cause Lists
- `GET /api/v1/folders` & `POST /api/v1/folders` — Folder management
- `PATCH /api/v1/folders/:id` & `DELETE /api/v1/folders/:id` — Rename & trash folder
- `GET /api/v1/cause-lists` & `POST /api/v1/cause-lists` — Cause list sessions
- `GET /api/v1/matters` & `POST /api/v1/matters` — Matters by date/folder
- `POST /api/v1/matters/:id/adjourn` — Adjourn matter to new date

### Recycle Bin
- `GET /api/v1/recycle-bin` — List soft-deleted transcripts
- `POST /api/v1/recycle-bin/:id/restore` — Restore transcript to active list
- `DELETE /api/v1/recycle-bin/:id/permanent` — Permanent purge
