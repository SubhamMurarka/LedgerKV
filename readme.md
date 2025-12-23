# LedgerKV

### Bitcask-Inspired Key-Value Store (Go)
A Bitcask-style persistent key–value store implemented in Go, designed to explore storage engine internals, write-ahead logging (WAL), garbage collection via compaction, and observability using Prometheus & Grafana.

![INDEX (2)](https://github.com/user-attachments/assets/d38c2b25-8ac9-4a3b-86ea-030e6bd4db21)

## Server Boot-up Flow

This section explains **what happens when the Bitcask server starts**, **in what order**, and **why each step exists**.  
Startup is one of the most critical parts of a storage engine because it is responsible for **crash recovery and correctness**.

The design follows a simple rule:

> **Disk is the source of truth. Memory is always derived.**

### High-Level Startup Sequence

On startup, the server performs the following steps **in strict order**:

1. Initialize directories and internal state  
2. Discover and order WAL files  
3. Rebuild the in-memory index from WAL  
4. Select and open the active WAL  
5. Start background services (metrics, compaction checks)  
6. Start the TCP server and accept client traffic  

Client requests are accepted **only after recovery is complete**.

### 1️⃣ Directory & State Initialization

At startup, the server ensures that the data directory exists.

- If the directory does not exist:
  - It is created
  - The system starts in a clean state
- If the directory exists:
  - All existing WAL files are treated as authoritative data

This guarantees that:
- The server can recover after crashes
- No in-memory state is trusted across restarts

### 2️⃣ WAL Discovery & Ordering

The server scans the data directory and discovers all WAL files.

WAL files follow a **monotonic naming scheme**:

wal-000001.log
wal-000002.log
wal-000003.log


Rules:
- WAL files are immutable once sealed
- File ordering is derived from the filename
- The WAL with the highest ID is considered the **active WAL**

Correct ordering is critical because:
- Writes must be replayed in the exact order they were issued
- Later records always override earlier ones

### 3️⃣ In-Memory Index Rebuild (Crash Recovery)

This is the **most important step** during boot-up.

The server rebuilds the in-memory index by **sequentially scanning WAL files from oldest to newest**.

For each record:
- `PUT` → index is updated to point to the latest `(fileID, offset)`
- `DEL` → a tombstone is recorded for the key
- Older entries automatically become garbage

The index maps:
key → (wal_file_id, offset, deleted)


Why a full WAL scan is required:
- Bitcask is append-only
- The index is a derived structure
- Disk data is always authoritative

This guarantees:
- Correct recovery after crashes
- No dependency on unsafe snapshots
- Deterministic startup behavior

### 4️⃣ Active WAL Selection

After all WAL files are scanned:
- The highest-numbered WAL file is selected as the **active WAL**
- If no WAL exists, a new WAL file is created

All new writes are appended **only** to the active WAL.

Important invariant:

> **The active WAL is never compacted.**

This simplifies concurrency and ensures:
- Writes are never blocked by compaction
- Predictable write behavior

### 5️⃣ Background Services Startup

Once recovery is complete, background services are started.

These include:
- Metrics endpoint (`/metrics`)
- WAL size collector
- Garbage ratio checker for compaction

Background services are started **after recovery** to ensure:
- They operate on a consistent state
- No recovery logic is interleaved with runtime behavior

### 6️⃣ TCP Server Startup

Only after:
- WAL discovery
- Index rebuild
- Active WAL selection

does the server start accepting client requests.

The TCP server listens for:

PUT key value
GET key
DEL key


This guarantees:
- Reads always return correct data
- Writes never race with recovery
- Clients never see a partially recovered state

### Why Startup Is Designed This Way

The boot-up design prioritizes:

- Correctness over speed
- Deterministic recovery
- Simple failure semantics
- Clear separation between recovery and runtime logic

This mirrors the startup behavior of real storage systems such as:
- Bitcask
- Log-structured databases
- Write-ahead log–based systems

## Possible Optimization: Hint Files

### Problem with Full WAL Scan

During server boot-up, the current recovery process **rebuilds the in-memory index by scanning all WAL files**.  
While this approach is **correct and simple**, it has a clear limitation:

- Startup time grows linearly with WAL size
- Large datasets result in slower recovery
- Full disk scan is required even when most data is stable

This is acceptable for learning and correctness, but real-world systems optimize this path.

### What Is a Hint File?

A **hint file** is a compact, on-disk representation of the in-memory index.

## Write Path: PUT, UPDATE, DELETE

This key-value store follows a **Bitcask-style append-only design**.  
All mutations are written sequentially to disk, and an in-memory index is used to locate the latest value for a key.

The **Write-Ahead Log (WAL)** is the **single source of truth**.

---

### On-Disk Record Format

Each write is stored as a single append-only record:

| CRC (4B) | KeySize (4B) | ValueSize (4B) | Key | Value |


- `ValueSize = -1` indicates a **tombstone** (delete)
- Records are never modified in place
- Older versions remain on disk as garbage

---

### In-Memory Index

The in-memory index maps:

key → (fileID, offset, deleted)


- Points to the **latest version** of a key
- Updated only after a successful WAL append
- Rebuilt on startup by replaying WAL files

---

## PUT Operation

**Goal:** Insert a new key or overwrite an existing key.

### Steps

1. Construct a record containing the key and value.
2. Append the record to the active WAL file:
   - Write header (key size, value size, CRC)
   - Write key and value bytes
   - `fsync()` to ensure durability
3. Update the in-memory index to point to the new `(fileID, offset)`.

### Properties

- Sequential disk write (fast on HDD and SSD)
- Durable once `fsync` completes
- Old values are not removed immediately
- Latest value is always resolved via the index

### Complexity

- **Time:** O(1) amortized
- **Disk IO:** Append-only
- **Memory:** One index entry per key

---

## UPDATE Operation

**UPDATE is treated exactly the same as PUT.**

There is no special update path.

### Behavior

- A new record with the same key is appended to the WAL
- The index is updated to point to the new record
- Older records become **garbage** and are ignored

wal.log:
k1 = v1 ← old (garbage)
k1 = v2 ← latest


Index:

k1 → (fileID, offset_of_v2)

### Why this works

- Avoids random disk writes
- Simplifies crash recovery
- Enables high write throughput

This is a core Bitcask design principle.

---

## DELETE Operation

**Goal:** Logically delete a key in a durable way.

### Steps

1. Append a **tombstone record** to the WAL:
   - Key is written
   - ValueSize is set to `-1`
2. Mark the key as deleted in the in-memory index.

### On-Disk Representation

| CRC | KeySize | -1 | Key |

No value bytes are written.

### Read Behavior

- If the index marks a key as deleted, `GET` returns `NOT FOUND`
- Physical removal of deleted keys is deferred to compaction

### Why tombstones?

- Deletes are durable
- Deletes survive crashes
- Old values are safely ignored
- Cleanup is handled asynchronously

---

## Crash Safety Guarantees

| Scenario | Result |
|--------|--------|
| Crash before WAL append | Operation is lost |
| Crash after WAL append | Operation is recovered |
| Partial write | CRC mismatch, record ignored |
| Crash during delete | Tombstone is replayed |

On startup, WAL files are replayed sequentially to rebuild the in-memory index.

---

## Relationship with Compaction

- PUT, UPDATE, and DELETE never remove data immediately
- Old records accumulate as garbage
- Compaction rewrites only **live records**
- Index is atomically updated during compaction

Writes and compaction are fully decoupled.

---

## Summary

PUT, UPDATE, and DELETE operations are implemented as append-only WAL writes, with an in-memory index pointing to the latest record. Old values and tombstones are cleaned up asynchronously via compaction, ensuring durability, simplicity, and high write throughput.

## GET Operation

**Goal:** Retrieve the latest value associated with a key.

The read path is optimized for **low latency** by leveraging the in-memory index.  
Disk is accessed only after the exact location of the record is known.

---

### Read Path Overview

1. Lookup the key in the in-memory index.
2. If the key does not exist or is marked as deleted, return `NOT FOUND`.
3. Use the stored `(fileID, offset)` to locate the record on disk.
4. Seek directly to the offset and read the record.
5. Validate the record using CRC.
6. Return the value.

## Compaction

This storage engine follows an **append-only write model**, which means old versions of keys and tombstone records accumulate over time.  
**Compaction** is the background process responsible for reclaiming this disk space.

Compaction rewrites only the **live records** into a new WAL file and deletes obsolete data.

---

### Why Compaction Is Needed

Because PUT, UPDATE, and DELETE never modify data in place:

- Old versions of keys remain on disk
- Deleted keys leave behind tombstones
- Disk usage grows monotonically

Without compaction, disk usage would grow without bound.

---

### Garbage Definition

A record is considered **garbage** if:

- It is not the latest version of its key
- OR it is a tombstone
- OR the index no longer points to its `(fileID, offset)`

Only records currently referenced by the in-memory index are considered **live**.

garbageRatio = (totalBytes - liveBytes) / totalBytes

### Compaction Trigger

Compaction is triggered when the **global garbage ratio** crosses a threshold.


- `totalBytes`: total size of all WAL files
- `liveBytes`: total size of records referenced by the index

When `garbageRatio ≥ threshold` (e.g., 50%), compaction is scheduled.

> Compaction is **not triggered on the write path**.  
> It runs in the background to avoid impacting PUT or GET latency.

---

### High-Level Compaction Algorithm

1. **Rotate the active WAL**
   - Freezes the current WAL so it becomes immutable
   - New writes continue in a fresh WAL

2. **Scan immutable WAL files**
   - Only WAL files with `fileID < activeID` are compacted
   - Active WAL is never compacted

3. **Copy live records**
   - Each record is validated using a CAS check against the index
   - Only records still referenced by the index are copied

4. **Write to a new compacted WAL**
   - Live records are appended sequentially
   - New offsets are recorded

5. **Atomically update the index**
   - Index entries are updated to point to the compacted WAL

6. **fsync and promote**
   - Compacted WAL is synced and atomically renamed

7. **Delete old WAL files**
   - All compacted WALs are removed safely

---

### Correctness Guarantees

- Active WAL is never compacted
- Index updates are atomic
- Reads always resolve to valid records
- Writes continue uninterrupted during compaction
- Only one compaction runs at a time

If the system crashes during compaction:
- Old WAL files remain intact
- Index is rebuilt from WALs on restart
- No data loss occurs


### Disk Usage Behavior (Saw-Tooth Pattern)

---<img width="1119" height="507" alt="Screenshot from 2025-12-17 15-48-52" src="https://github.com/user-attachments/assets/e6dde090-f0b8-4706-985a-21f181d4941d" />

Because compaction rewrites only live data:

- Disk usage **steadily increases** as writes append
- When compaction finishes, disk usage **drops sharply**

This produces a characteristic **saw-tooth pattern** in disk usage over time.


This saw-tooth behavior is **expected and desirable**, and is a strong indicator that compaction is functioning correctly.

> In a Bitcask-style system, the saw-tooth is primarily observed in **disk usage**, not heap memory, since values are stored on disk and only the index resides in memory.

---

### Performance Characteristics

- Compaction is sequential IO heavy
- CPU usage spikes during compaction
- PUT and GET latency remain stable
- No random disk writes are introduced

---

### Summary

Compaction is a background process that reclaims disk space by rewriting only live records into a new WAL file and deleting obsolete data. It is triggered based on garbage ratio, runs without blocking writes or reads, and produces a characteristic saw-tooth disk usage pattern that reflects healthy system behavior.


# Tech Stack
![Go](https://img.shields.io/badge/Go-00ADD8?style=for-the-badge&logo=go&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-2496ED?style=for-the-badge&logo=docker&logoColor=white)
![Docker Compose](https://img.shields.io/badge/Docker_Compose-2496ED?style=for-the-badge&logo=docker&logoColor=white)
![Prometheus](https://img.shields.io/badge/Prometheus-E6522C?style=for-the-badge&logo=prometheus&logoColor=white)
![Grafana](https://img.shields.io/badge/Grafana-F46800?style=for-the-badge&logo=grafana&logoColor=white)

# 🔧 Setup Instructions

To run the LedgerKV app backend on your machine:

1. Clone the repository:
   ```bash
   git clone https://github.com/SubhamMurarka/LedgerKV.git

2. Run with Docker:
   ```bash
   docker-compose up -d --build

3. Setup TCP connection:
  ```bash
 nc localhost 7379
