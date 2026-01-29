package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	// Key prefixes
	KeyPrefixTasks   = "/lpmos/tasks/"
	KeyPrefixRegions = "/lpmos/regions/"
	KeyPrefixAgents  = "/lpmos/agents/"
	KeyPrefixConfig  = "/lpmos/config/"

	// Default timeouts
	DefaultDialTimeout    = 5 * time.Second
	DefaultRequestTimeout = 10 * time.Second
)

// Client wraps etcd client with LPMOS-specific operations
type Client struct {
	cli            *clientv3.Client
	requestTimeout time.Duration
}

// Config holds etcd client configuration
type Config struct {
	Endpoints      []string
	DialTimeout    time.Duration
	RequestTimeout time.Duration
	Username       string
	Password       string
	// TLS configuration can be added here
}

// NewClient creates a new etcd client
func NewClient(cfg Config) (*Client, error) {
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = DefaultDialTimeout
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = DefaultRequestTimeout
	}

	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: cfg.DialTimeout,
		Username:    cfg.Username,
		Password:    cfg.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	return &Client{
		cli:            cli,
		requestTimeout: cfg.RequestTimeout,
	}, nil
}

// Close closes the etcd client connection
// Should be called when the client is no longer needed to release resources
func (c *Client) Close() error {
	if c.cli != nil {
		return c.cli.Close()
	}
	return nil
}

// Put stores a value in etcd with automatic context timeout
func (c *Client) Put(key string, value interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	defer cancel()

	var valueStr string
	switch v := value.(type) {
	case string:
		valueStr = v
	case []byte:
		valueStr = string(v)
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("failed to marshal value: %w", err)
		}
		valueStr = string(data)
	}

	_, err := c.cli.Put(ctx, key, valueStr)
	if err != nil {
		return fmt.Errorf("failed to put key %s: %w", key, err)
	}

	return nil
}

// Get retrieves a value from etcd
func (c *Client) Get(key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	defer cancel()

	resp, err := c.cli.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", key, err)
	}

	if len(resp.Kvs) == 0 {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	return resp.Kvs[0].Value, nil
}

// GetJSON retrieves and unmarshals a JSON value from etcd
func (c *Client) GetJSON(key string, target interface{}) error {
	data, err := c.Get(key)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("failed to unmarshal value: %w", err)
	}

	return nil
}

// GetWithPrefix retrieves all keys with a given prefix
func (c *Client) GetWithPrefix(prefix string) (map[string][]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	defer cancel()

	resp, err := c.cli.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to get keys with prefix %s: %w", prefix, err)
	}

	result := make(map[string][]byte)
	for _, kv := range resp.Kvs {
		result[string(kv.Key)] = kv.Value
	}

	return result, nil
}

// Delete removes a key from etcd
func (c *Client) Delete(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	defer cancel()

	_, err := c.cli.Delete(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to delete key %s: %w", key, err)
	}

	return nil
}

// Watch watches for changes on a key or prefix
func (c *Client) Watch(ctx context.Context, key string, prefix bool) clientv3.WatchChan {
	if prefix {
		return c.cli.Watch(ctx, key, clientv3.WithPrefix())
	}
	return c.cli.Watch(ctx, key)
}

// PutWithLease stores a value with a TTL lease
func (c *Client) PutWithLease(key string, value interface{}, ttlSeconds int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	defer cancel()

	// Create lease
	lease, err := c.cli.Grant(ctx, ttlSeconds)
	if err != nil {
		return fmt.Errorf("failed to create lease: %w", err)
	}

	var valueStr string
	switch v := value.(type) {
	case string:
		valueStr = v
	case []byte:
		valueStr = string(v)
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("failed to marshal value: %w", err)
		}
		valueStr = string(data)
	}

	_, err = c.cli.Put(ctx, key, valueStr, clientv3.WithLease(lease.ID))
	if err != nil {
		return fmt.Errorf("failed to put key with lease: %w", err)
	}

	return nil
}

// Transaction executes multiple operations atomically
func (c *Client) Transaction(ops []clientv3.Op) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	defer cancel()

	_, err := c.cli.Txn(ctx).Then(ops...).Commit()
	if err != nil {
		return fmt.Errorf("failed to execute transaction: %w", err)
	}

	return nil
}

// Helper functions for building keys

// TaskKey builds a task key path
func TaskKey(taskID string, suffix ...string) string {
	key := KeyPrefixTasks + taskID
	for _, s := range suffix {
		key += "/" + s
	}
	return key
}

// RegionKey builds a region key path
func RegionKey(regionID string, suffix ...string) string {
	key := KeyPrefixRegions + regionID
	for _, s := range suffix {
		key += "/" + s
	}
	return key
}

// AgentKey builds an agent key path
func AgentKey(macAddr string, suffix ...string) string {
	key := KeyPrefixAgents + macAddr
	for _, s := range suffix {
		key += "/" + s
	}
	return key
}

// IsKeyNotFound checks if error is key not found
func IsKeyNotFound(err error) bool {
	return err != nil && err.Error() == "key not found"
}
