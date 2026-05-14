# ENGRAM-HYBRID PROJECT BRIEF

## 🎯 Mission
Build **Engram-Hybrid**: a persistent memory system for AI coding agents that combines structured full-text search with semantic vector search, organized in global and project tiers.

## 🌍 Context
- **Origin**: Fork of [Gentleman-Programming/engram](https://github.com/Gentleman-Programming/engram) (MIT license, 3.5k stars)
- **Goal**: Add Qdrant vector search + global/project memory hierarchy without changing the core SQLite/FTS5 layer
- **Cost**: 100% free forever. Everything runs locally on user's existing infrastructure.

## 🖥️ Infrastructure (User's Environment)
| Service | Type | Port | Purpose |
|---|---|---|---|
| Qdrant | Docker container (`qdrant-godot`) | 6333 | Vector DB (HNSW index) |
| Ollama | Windows host app | 11434 | Embeddings (`nomic-embed-text:latest`) |
| llama.cpp | Docker container (`turboquant`) | 8080 | LLM serving (Qwen3.6-35B) |
| SearXNG | Docker container (`Search-MCP`) | 8888 | Web search (optional) |

**Disk**: 20TB HDD for raw content. Qdrant index should ideally go on SSD if available, but HDD works for now.

## 📐 Architecture Decision
```
┌─────────────────────────────────────────────────┐
│              Agent (opencode)                   │
│         MCP stdio transport                     │
└──────────────┬──────────────────────────────────┘
               │
    ┌──────────▼──────────┐  ┌────────▼──────────────┐
    │   Global Memory      │  │  Project Memory       │
    │  SQLite + Qdrant     │  │  SQLite + Qdrant      │
    └──────────┬──────────┘  └────────┬──────────────┘
               │                      │
    ┌──────────▼──────────────────────▼──────────────┐
    │          Memory Orchestrator                    │
    │  - Hybrid search (FTS5 BM25 + Qdrant vector)  │
    │  - Reciprocal Rank Fusion for ranking         │
    │  - Async embedding generation                 │
    │  - Auto-compaction / summarization            │
    └──────────┬─────────────────────────────────────┘
               │
    ┌──────────▼──────────┐  ┌──────────────────────┐
    │  Qdrant (Docker)     │  │  HDD (20TB raw)      │
    │  HNSW semantic index │  │  SQLite + documents  │
    └─────────────────────┘  └──────────────────────┘
```

## 🔑 Engram Codebase Analysis (Source Repo)
**Key files to understand:**
- `internal/store/store.go` (6343 lines) — SQLite schema, migrations, FTS5 search
- `internal/mcp/mcp.go` (2624 lines) — 19 MCP tools, project resolution logic
- `cmd/engram/main.go` (2000+ lines) — CLI entry point with subcommands
- `go.mod` — `modernc.org/sqlite`, `mark3labs/mcp-go`, `charmbracelet/bubbles`

**Critical findings:**
1. **Dormant embedding columns exist**: `embedding BLOB`, `embedding_model TEXT`, `embedding_created_at TEXT` on `observations` table (added in conflict-surfacing phase but never used)
2. **Single SQLite connection**: `SetMaxOpenConns(1)` — embedding generation MUST be async via channel/goroutine
3. **Cross-project relations blocked**: `ErrCrossProjectRelation` must be removed for global memory to work
4. **FTS5 triggers are fragile**: Don't modify existing triggers; add new tables separately
5. **Sync payload bloat risk**: Embeddings (~6KB each) would bloat cloud sync mutations — keep embeddings local-only

## 🧱 What We're Building On Top of Engram
| Layer | Storage | Purpose |
|---|---|---|
| Global memory | `global_observations` table + Qdrant collection | Cross-project knowledge, principles, tech stack |
| Project memory | `observations` table + Qdrant collection | Per-project decisions, bugs, learnings |
| Vector index | Qdrant Docker container | Semantic search across all tiers |
| Raw content | HDD (20TB) | Full text documents, diffs, summaries |

## 🛠️ Implementation Plan (Phased)

### Phase 1: Foundation (Qdrant Client + Embeddings)
- [ ] Fork engram → `engram-hybrid`
- [ ] Create `internal/vector/qdrant.go` — Qdrant Go client wrapper
- [ ] Create `internal/vector/embed.go` — Ollama embedding generator abstraction
- [ ] Add `qdrant-go` and `ollama` dependencies to go.mod
- [ ] Create `vector_index_meta` table in store (observation_id → qdrant_point_id mapping)
- [ ] Update `AddObservation()` to generate embeddings async after insert

### Phase 2: Hybrid Search
- [ ] Add `SearchHybrid()` method that combines FTS5 + Qdrant results
- [ ] Implement Reciprocal Rank Fusion (RRF) for ranking
- [ ] Add `mem_vector_search` MCP tool with `semantic`, `semantic_weight` params
- [ ] Update existing `mem_search` to support hybrid mode

### Phase 3: Global Memory Hierarchy
- [ ] Create `global_observations` table (separate from project-scoped)
- [ ] Add `mem_global_save` and `mem_global_search` MCP tools
- [ ] Update search to optionally include global scope
- [ ] Remove `ErrCrossProjectRelation` check

### Phase 4: Compaction & Context
- [ ] Add `memory_compactions` table for summarization
- [ ] Create `internal/store/compaction.go` — auto-summarize old memories
- [ ] Add `mem_compact` MCP tool
- [ ] Enhance `mem_context` to fetch global + project relevant memory

## ⚠️ Gotchas & Anti-Patterns
1. **SQLite single connection**: Never block the store thread with embedding generation. Use buffered channel + goroutine.
2. **Vector serialization**: Store `[]float32` as `BLOB` using `binary.LittleEndian`. Qdrant expects raw binary.
3. **FTS5 trigger safety**: Don't modify existing triggers (obs_fts_insert/update/delete). New tables get their own triggers.
4. **Embedding model mismatch**: Ollama uses `nomic-embed-text:latest` (768-dim, Cosine distance). Hardcode this in config.
5. **Backward compatibility**: All new MCP tools must be additive. Existing tools keep working unchanged.
6. **Error handling**: Embedding failures are non-fatal — log and continue. Never block `mem_save`.

## 📦 Dependencies to Add
```go
github.com/qdrant/qdrant-go   // Qdrant Go client (v1.11+)
github.com/ollama/ollama/api  // Ollama HTTP API client
```

## 🚀 Quick Start for New Agent
```bash
# 1. Clone the fork
git clone https://github.com/<username>/engram-hybrid.git
cd engram-hybrid

# 2. Verify Qdrant is running
curl http://localhost:6333/

# 3. Verify Ollama is running
curl http://localhost:11434/api/version

# 4. Build
go mod tidy
go build -o engram-hybrid ./cmd/engram/

# 5. Initialize
./engram-hybrid init
./engram-hybrid vector init   # Creates Qdrant collections
```

## 💡 Design Principles
- **Additive only**: Never break existing tools or CLI commands
- **Local-first**: Zero external API calls by default
- **Async embeddings**: Never block SQLite's single connection
- **Pluggable embedder**: Swap Ollama → OpenAI → local GGML via config
- **Schema-safe**: All new tables use `CREATE TABLE IF NOT EXISTS`
- **Graceful degradation**: If Qdrant is down, fall back to pure FTS5 search
