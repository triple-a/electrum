package electrum

import (
	"database/sql"
	"encoding/json"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

type TxCache struct {
	mu sync.Mutex
	db *sql.DB
}

func NewTxCache(db *sql.DB) (*TxCache, error) {
	if db == nil {
		var err error
		db, err = sql.Open("sqlite3", "tx_cache.db")
		if err != nil {
			return nil, err
		}
	}

	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS tx_cache (
		txid VARCHAR(64) PRIMARY KEY,
		tx TEXT,
		is_detailed INTEGER DEFAULT 0
	)
	`)
	if err != nil {
		return nil, err
	}
	return &TxCache{db: db}, nil
}

func (c *TxCache) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

func (c *TxCache) Store(txID string, tx any) error {
	b, err := json.Marshal(tx)
	if err != nil {
		return err
	}

	isDetailed := 0
	switch tx.(type) {
	case *RichTx:
		isDetailed = 1
	case RichTx:
		isDetailed = 1
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	_, err = c.db.Exec(
		`INSERT INTO tx_cache (txid, tx, is_detailed) VALUES (?, ?, ?)
		ON CONFLICT(txid) WHERE is_detailed = 0 DO UPDATE SET
			tx = ?,
			is_detailed = ?`,
		txID,
		string(b[:]),
		isDetailed,
		string(b[:]),
		isDetailed,
	)
	if err != nil {
		return err
	}

	return nil
}

func (c *TxCache) Load(txID string, tx any) bool {
	c.mu.Lock()

	row, err := c.db.Query(
		"SELECT tx, is_detailed FROM tx_cache WHERE txid = ?",
		txID,
	)

	c.mu.Unlock()

	if err != nil {
		return false
	}
	defer row.Close()
	if !row.Next() {
		return false
	}
	var data []byte
	var isDetailed int
	err = row.Scan(&data, &isDetailed)
	if err != nil {
		return false
	}

	switch tx.(type) {
	case *RichTx:
		if isDetailed == 0 {
			return false
		}
	}
	err = json.Unmarshal(data, tx)

	return err == nil
}
