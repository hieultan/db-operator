package postgres

import (
	"database/sql"
	"errors"
)

type PostgreSQLClients map[string]*sql.DB

var ErrPostgreSQLClientNotFound = errors.New("PostgreSQL client not found")

func (m PostgreSQLClients) GetClient(key string) (*sql.DB, error) {
        dbClient, ok := m[key]
        if ok {
                return dbClient, nil
        } else {
                return nil, ErrPostgreSQLClientNotFound
        }
}

// Close a PostgreSQL client
func (m PostgreSQLClients) Close(name string) error {
        dbClient, ok := m[name]
        if !ok {
                return ErrPostgreSQLClientNotFound
        }
        if err := dbClient.Close(); err != nil {
                return err
        }
        delete(m, name)
        return nil
}

// Close all PostgreSQL clients.
// Return error immediately when error occurs for a client.
func (m PostgreSQLClients) CloseAll() error {
        for name := range m {
                err := m.Close(name)
		if err != nil {
			return err
		}
	}
	return nil
}
