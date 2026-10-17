use super::*;
use crate::auth::{protect, unprotect};
use rusqlite::{Connection, OptionalExtension, params};

#[derive(Clone)]
pub(super) struct Journal {
    file: PathBuf,
}
impl Journal {
    pub fn open(root: &Path) -> Result<Self, String> {
        fs::create_dir_all(root).map_err(|_| "journal_unavailable")?;
        let value = Self {
            file: root.join("executions.sqlite"),
        };
        value.db()?.execute_batch("PRAGMA journal_mode=WAL;
   CREATE TABLE IF NOT EXISTS executions(id TEXT PRIMARY KEY,generation TEXT NOT NULL,phase TEXT NOT NULL,config TEXT NOT NULL,pid INTEGER);
   CREATE TABLE IF NOT EXISTS execution_inputs(id TEXT PRIMARY KEY,payload BLOB NOT NULL);
   CREATE TABLE IF NOT EXISTS execution_logs(position INTEGER PRIMARY KEY AUTOINCREMENT,run_id TEXT NOT NULL,channel TEXT NOT NULL,payload BLOB NOT NULL);
   CREATE INDEX IF NOT EXISTS execution_log_page ON execution_logs(run_id,position);
   CREATE TABLE IF NOT EXISTS execution_frames(position INTEGER PRIMARY KEY AUTOINCREMENT,run_id TEXT NOT NULL,operation_id TEXT NOT NULL,payload BLOB NOT NULL,sent INTEGER NOT NULL DEFAULT 0);
   CREATE INDEX IF NOT EXISTS execution_pending ON execution_frames(run_id,sent,position);
CREATE INDEX IF NOT EXISTS execution_pending_exists ON execution_frames(sent) WHERE sent=0;
CREATE INDEX IF NOT EXISTS execution_unknown ON executions(phase) WHERE phase IN ('unknown','unconfirmed');
   CREATE TABLE IF NOT EXISTS execution_results(run_id TEXT PRIMARY KEY,payload BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS execution_responses(run_id TEXT NOT NULL,response_id TEXT NOT NULL,PRIMARY KEY(run_id,response_id));").map_err(|_|"journal_unavailable")?;
        Ok(value)
    }
    fn db(&self) -> Result<Connection, String> {
        let db = Connection::open(&self.file).map_err(|_| "journal_unavailable")?;
        db.busy_timeout(std::time::Duration::from_secs(5))
            .map_err(|_| "journal_unavailable")?;
        db.execute_batch("PRAGMA synchronous=FULL")
            .map_err(|_| "journal_unavailable")?;
        Ok(db)
    }
    pub fn begin(&self, id: &str, generation: &str, config: &str) -> Result<bool, String> {
        let mut db = self.db()?;
        let tx = db.transaction().map_err(|_| "journal_unavailable")?;
        let previous: Option<(String, String, String)> = tx
            .query_row(
                "SELECT generation,phase,config FROM executions WHERE id=?",
                [id],
                |r| Ok((r.get(0)?, r.get(1)?, r.get(2)?)),
            )
            .optional()
            .map_err(|_| "journal_unavailable")?;
        let allowed = match previous {
            None => true,
            Some((old, phase, hash)) => old == generation && phase == "blocked" && hash != config,
        };
        if allowed {
            tx.execute("INSERT INTO executions VALUES(?1,?2,'preparing',?3,NULL) ON CONFLICT(id) DO UPDATE SET phase='preparing',config=excluded.config",params![id,generation,config]).map_err(|_|"journal_unavailable")?;
        }
        tx.commit().map_err(|_| "journal_unavailable")?;
        Ok(allowed)
    }
    pub fn phase(&self, id: &str, phase: &str, pid: Option<u32>) -> Result<(), String> {
        self.db()?
            .execute(
                "UPDATE executions SET phase=?2,pid=COALESCE(?3,pid) WHERE id=?1",
                params![id, phase, pid],
            )
            .map_err(|_| "journal_unavailable")?;
        Ok(())
    }
    pub fn input(&self, id: &str, body: &Value) -> Result<(), String> {
        let encrypted = protect(&serde_json::to_vec(body).map_err(|_| "journal_encoding_failed")?)?;
        self.db()?
            .execute(
                "INSERT OR IGNORE INTO execution_inputs VALUES(?1,?2)",
                params![id, encrypted],
            )
            .map_err(|_| "journal_unavailable")?;
        Ok(())
    }
    pub fn log(&self, id: &str, channel: &str, bytes: &[u8]) -> Result<i64, String> {
        let encrypted = protect(bytes)?;
        let db = self.db()?;
        db.execute(
            "INSERT INTO execution_logs(run_id,channel,payload) VALUES(?1,?2,?3)",
            params![id, channel, encrypted],
        )
        .map_err(|_| "journal_unavailable")?;
        Ok(db.last_insert_rowid())
    }
    pub fn enqueue(&self, id: &str, body: &Value) -> Result<i64, String> {
        let encrypted = protect(&serde_json::to_vec(body).map_err(|_| "journal_encoding_failed")?)?;
        let db = self.db()?;
        db.execute(
            "INSERT INTO execution_frames(run_id,operation_id,payload) VALUES(?1,?2,?3)",
            params![id, Uuid::new_v4().to_string(), encrypted],
        )
        .map_err(|_| "journal_unavailable")?;
        Ok(db.last_insert_rowid())
    }
    pub fn pending(&self, id: &str) -> Result<Option<(i64, String, Value)>, String> {
        let row:Option<(i64,String,Vec<u8>)>=self.db()?.query_row("SELECT position,operation_id,payload FROM execution_frames WHERE run_id=?1 AND sent=0 ORDER BY position LIMIT 1",[id],|r|Ok((r.get(0)?,r.get(1)?,r.get(2)?))).optional().map_err(|_|"journal_unavailable")?;
        row.map(|(n, key, data)| {
            Ok((
                n,
                key,
                serde_json::from_slice(&unprotect(&data)?).map_err(|_| "invalid_journal")?,
            ))
        })
        .transpose()
    }
    pub fn attempted(&self, n: i64, body: &Value) -> Result<(), String> {
        let encrypted = protect(&serde_json::to_vec(body).map_err(|_| "journal_encoding_failed")?)?;
        self.db()?
            .execute(
                "UPDATE execution_frames SET payload=?2 WHERE position=?1",
                params![n, encrypted],
            )
            .map_err(|_| "journal_unavailable")?;
        Ok(())
    }
    pub fn sent(&self, n: i64) -> Result<(), String> {
        self.db()?
            .execute("UPDATE execution_frames SET sent=1 WHERE position=?1", [n])
            .map_err(|_| "journal_unavailable")?;
        Ok(())
    }
    pub fn claim_response(&self, id: &str, response: &str) -> Result<bool, String> {
        Ok(self
            .db()?
            .execute(
                "INSERT OR IGNORE INTO execution_responses VALUES(?1,?2)",
                params![id, response],
            )
            .map_err(|_| "journal_unavailable")?
            == 1)
    }
    pub fn result(&self, id: &str, value: &Value) -> Result<(), String> {
        let data = protect(&serde_json::to_vec(value).map_err(|_| "journal_encoding_failed")?)?;
        self.db()?.execute("INSERT INTO execution_results VALUES(?1,?2) ON CONFLICT(run_id) DO UPDATE SET payload=excluded.payload",params![id,data]).map_err(|_|"journal_unavailable")?;
        Ok(())
    }
    pub fn final_result(&self, id: &str) -> Result<Option<Value>, String> {
        let data: Option<Vec<u8>> = self
            .db()?
            .query_row(
                "SELECT payload FROM execution_results WHERE run_id=?1",
                [id],
                |r| r.get(0),
            )
            .optional()
            .map_err(|_| "journal_unavailable")?;
        data.map(|v| serde_json::from_slice(&unprotect(&v)?).map_err(|_| "invalid_journal".into()))
            .transpose()
    }
    pub fn recover_unknown(&self) -> Result<Vec<(String, String)>, String> {
        let mut db = self.db()?;
        let tx = db.transaction().map_err(|_| "journal_unavailable")?;
        let rows = {
            let mut q=tx.prepare("SELECT id,generation FROM executions WHERE phase IN ('preparing','starting','running','unconfirmed')").map_err(|_|"journal_unavailable")?;
            q.query_map([], |r| Ok((r.get::<_, String>(0)?, r.get::<_, String>(1)?)))
                .map_err(|_| "journal_unavailable")?
                .collect::<Result<Vec<_>, _>>()
                .map_err(|_| "journal_unavailable")?
        };
        tx.execute("UPDATE executions SET phase='unknown' WHERE phase IN ('preparing','starting','running','unconfirmed')",[]).map_err(|_|"journal_unavailable")?;
        tx.commit().map_err(|_| "journal_unavailable")?;
        Ok(rows)
    }
    pub fn summary(&self) -> Result<Value, String> {
        let db = self.db()?;
        let pending: bool = db
            .query_row(
                "SELECT EXISTS(SELECT 1 FROM execution_frames WHERE sent=0)",
                [],
                |r| r.get(0),
            )
            .map_err(|_| "journal_unavailable")?;
        let unknown: bool = db
            .query_row(
                "SELECT EXISTS(SELECT 1 FROM executions WHERE phase IN ('unknown','unconfirmed'))",
                [],
                |r| r.get(0),
            )
            .map_err(|_| "journal_unavailable")?;
        Ok(json!({"pending_frames":pending,"unconfirmed_execution":unknown}))
    }
}

#[cfg(all(test, windows))]
mod tests {
    use super::*;
    #[test]
    fn records_survive_restart_without_restarting_a_process_or_exposing_payloads() {
        let root =
            std::env::temp_dir().join(format!("koujou-execution-journal-{}", Uuid::new_v4()));
        let journal = Journal::open(&root).unwrap();
        assert!(journal.begin("run:one", "generation", "config-v1").unwrap());
        let text = "SYNTHETIC_PRIVATE_EXECUTION_PAYLOAD";
        journal.input("run:one", &json!({"prompt":text})).unwrap();
        journal.log("run:one", "stdout", text.as_bytes()).unwrap();
        let position = journal
            .enqueue("run:one", &json!({"type":"output","value":text}))
            .unwrap();
        let pending = journal.pending("run:one").unwrap().unwrap();
        assert_eq!(pending.2["value"], text);
        journal.attempted(position,&json!({"type":"output","value":text,"operation_id":pending.1,"sequence":"1","expected_version":"fixed"})).unwrap();
        journal.phase("run:one", "running", Some(123)).unwrap();
        drop(journal);
        let reopened = Journal::open(&root).unwrap();
        assert_eq!(
            reopened.recover_unknown().unwrap(),
            vec![("run:one".into(), "generation".into())]
        );
        assert!(
            !reopened
                .begin("run:one", "generation", "config-v2")
                .unwrap()
        );
        let saved = reopened.pending("run:one").unwrap().unwrap();
        assert_eq!(saved.1, pending.1);
        assert_eq!(saved.2["expected_version"], "fixed");
        for name in ["executions.sqlite", "executions.sqlite-wal"] {
            if let Ok(data) = fs::read(root.join(name)) {
                assert!(!data.windows(text.len()).any(|b| b == text.as_bytes()));
            }
        }
        reopened.sent(position).unwrap();
        assert!(reopened.pending("run:one").unwrap().is_none());
        assert!(root.starts_with(std::env::temp_dir()));
        fs::remove_dir_all(root).unwrap();
    }
}
