package authoritytest

import (
	"context"
	"database/sql/driver"
)

// CommitFaultConnector must wrap the connector before the single registered
// pool is opened. It never substitutes a transaction or creates a second pool.
func CommitFaultConnector(inner driver.Connector) driver.Connector {
	return faultConnector{inner: inner}
}

type faultConnector struct{ inner driver.Connector }
type faultDriver struct{ driver.Driver }

func (c faultConnector) Driver() driver.Driver { return faultDriver{c.inner.Driver()} }
func (c faultConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &faultConn{Conn: conn}, nil
}

type faultConn struct{ driver.Conn }

func (c *faultConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	// All supported native fixture drivers implement ConnBeginTx; do not
	// silently discard transaction options on an unsupported driver.
	begin, ok := c.Conn.(driver.ConnBeginTx)
	if !ok {
		return nil, driver.ErrSkip
	}
	tx, err := begin.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	if fault, ok := ctx.Value(commitContextKey{}).(*commitFault); ok {
		return &sqliteTx{Tx: tx, fault: fault}, nil
	}
	return tx, nil
}

func (c *faultConn) Ping(ctx context.Context) error {
	if p, ok := c.Conn.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}
func (c *faultConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if p, ok := c.Conn.(driver.ConnPrepareContext); ok {
		return p.PrepareContext(ctx, query)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.Conn.Prepare(query)
}
func (c *faultConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if e, ok := c.Conn.(driver.ExecerContext); ok {
		return e.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}
func (c *faultConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if q, ok := c.Conn.(driver.QueryerContext); ok {
		return q.QueryContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}
func (c *faultConn) CheckNamedValue(value *driver.NamedValue) error {
	if check, ok := c.Conn.(driver.NamedValueChecker); ok {
		return check.CheckNamedValue(value)
	}
	return driver.ErrSkip
}
func (c *faultConn) ResetSession(ctx context.Context) error {
	if reset, ok := c.Conn.(driver.SessionResetter); ok {
		return reset.ResetSession(ctx)
	}
	return nil
}
func (c *faultConn) IsValid() bool {
	if validator, ok := c.Conn.(driver.Validator); ok {
		return validator.IsValid()
	}
	return true
}
