package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

const (
	// Key prefixes (updated for region-based structure)
	KeyPrefixTasks          = "/os/install/task/"
	KeyPrefixRegions        = "/os/region/"
	KeyPrefixUnmatchedReports = "/os/unmatched_reports/"

	// OPTIMIZED SCHEMA v3.0 key prefixes
	KeyPrefixServers        = "/os/%s/servers/"          // Individual server keys
	KeyPrefixMachines       = "/os/%s/machines//"         // Machine details
	KeyPrefixGlobalStats    = "/os/global/stats/"        // Cross-IDC stats

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

// GetClient returns the underlying etcd client
// Use with caution - only for operations not wrapped by this client
func (c *Client) GetClient() *clientv3.Client {
	return c.cli
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

// GetWithVersion retrieves a value and its version (for CAS operations)
func (c *Client) GetWithVersion(key string) ([]byte, int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	defer cancel()

	resp, err := c.cli.Get(ctx, key)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get key %s: %w", key, err)
	}

	if len(resp.Kvs) == 0 {
		return nil, 0, fmt.Errorf("key not found: %s", key)
	}

	return resp.Kvs[0].Value, resp.Kvs[0].Version, nil
}

// Txn creates a new transaction
func (c *Client) Txn(ctx context.Context) clientv3.Txn {
	return c.cli.Txn(ctx)
}

// AtomicUpdate performs an atomic read-modify-write with version check (v3.0)
func (c *Client) AtomicUpdate(key string, updateFn func([]byte) (interface{}, error)) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.requestTimeout)
	defer cancel()

	for retries := 0; retries < 3; retries++ {
		// Get current version
		data, version, err := c.GetWithVersion(key)
		if err != nil {
			return err
		}

		// Apply update function
		newValue, err := updateFn(data)
		if err != nil {
			return err
		}

		// Marshal new value
		var valueStr string
		switch v := newValue.(type) {
		case string:
			valueStr = v
		case []byte:
			valueStr = string(v)
		default:
			jsonData, err := json.Marshal(newValue)
			if err != nil {
				return fmt.Errorf("failed to marshal value: %w", err)
			}
			valueStr = string(jsonData)
		}

		// Atomic compare-and-swap
		txn := c.cli.Txn(ctx)
		resp, err := txn.
			If(clientv3.Compare(clientv3.Version(key), "=", version)).
			Then(clientv3.OpPut(key, valueStr)).
			Else(clientv3.OpGet(key)).
			Commit()

		if err != nil {
			return fmt.Errorf("transaction failed: %w", err)
		}

		if resp.Succeeded {
			return nil // Success!
		}

		// Conflict - retry
		time.Sleep(time.Duration(retries+1) * 100 * time.Millisecond)
	}

	return fmt.Errorf("failed after 3 retries due to conflicts")
}

// GrantLease creates a new lease with keep-alive (v3.0)
func (c *Client) GrantLease(ctx context.Context, ttlSeconds int64) (clientv3.LeaseID, <-chan *clientv3.LeaseKeepAliveResponse, error) {
	lease, err := c.cli.Grant(ctx, ttlSeconds)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to create lease: %w", err)
	}

	keepAliveChan, err := c.cli.KeepAlive(ctx, lease.ID)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to keep-alive lease: %w", err)
	}

	return lease.ID, keepAliveChan, nil
}

// Helper functions for building keys

// TaskKey builds a task key path with region
// Example: TaskKey("dc1", "task-123", "status") -> "/os/install/task/dc1/task-123/status"
func TaskKey(regionID string, taskID string, suffix ...string) string {
	key := KeyPrefixTasks + regionID + "/" + taskID
	for _, s := range suffix {
		key += "/" + s
	}
	return key
}

// TaskKeyPrefix returns the prefix for all tasks in a region
// Example: TaskKeyPrefix("dc1") -> "/os/install/task/dc1/"
func TaskKeyPrefix(regionID string) string {
	return KeyPrefixTasks + regionID + "/"
}

// RegionKey builds a region key path
// Example: RegionKey("dc1", "heartbeat") -> "/os/region/dc1/heartbeat"
func RegionKey(regionID string, suffix ...string) string {
	key := KeyPrefixRegions + regionID
	for _, s := range suffix {
		key += "/" + s
	}
	return key
}

// UnmatchedReportKey builds an unmatched report key
// Example: UnmatchedReportKey("dc1", "20260130-fe:b7:02:c0:95:e0")
func UnmatchedReportKey(regionID string, identifier string) string {
	return KeyPrefixUnmatchedReports + regionID + "/" + identifier
}

// AgentKey builds an agent key path (deprecated, kept for compatibility)
func AgentKey(macAddr string, suffix ...string) string {
	key := "/os/agents/" + macAddr
	for _, s := range suffix {
		key += "/" + s
	}
	return key
}

// IsKeyNotFound checks if error is key not found
func IsKeyNotFound(err error) bool {
	return err != nil && err.Error() == "key not found"
}

// OPTIMIZED SCHEMA v3.0 key helpers

// ServerKey builds a server key path (v3.0 individual keys)
// Example: ServerKey("dc1", "sn-001") -> "/os/dc1/servers/sn-001"
func ServerKey(idc string, sn string) string {
	return fmt.Sprintf("/os/%s/servers/%s", idc, sn)
}

// ServerPrefix returns the prefix for all servers in an IDC (v3.0)
// Example: ServerPrefix("dc1") -> "/os/dc1/servers/"
func ServerPrefix(idc string) string {
	return fmt.Sprintf(KeyPrefixServers, idc)
}

// MachineKey builds a machine key path (v3.0)
// Example: MachineKey("dc1", "sn-001", "meta") -> "/os/dc1/machines/sn-001/meta"
func MachineKey(idc string, sn string, suffix string) string {
	return fmt.Sprintf("/os/%s/machines/%s/%s", idc, sn, suffix)
}

// MachinePrefix returns the prefix for all machines in an IDC (v3.0)
// Example: MachinePrefix("dc1") -> "/os/dc1/machines/"
func MachinePrefix(idc string) string {
	return fmt.Sprintf("/os/%s/machines/", idc)
}

// TaskKeyV3 builds the task key path (merged task + state) (v3.0)
// Example: TaskKeyV3("dc1", "sn-001") -> "/os/dc1/machines/sn-001/task"
func TaskKeyV3(idc string, sn string) string {
	return MachineKey(idc, sn, "task")
}

// MetaKey builds the hardware metadata key path (v3.0)
// Example: MetaKey("dc1", "sn-001") -> "/os/dc1/machines/sn-001/meta"
func MetaKey(idc string, sn string) string {
	return MachineKey(idc, sn, "meta")
}

// LeaseKey builds the lease key path for heartbeats (v3.0)
// Example: LeaseKey("dc1", "sn-001") -> "/os/dc1/machines/sn-001/lease"
func LeaseKey(idc string, sn string) string {
	return MachineKey(idc, sn, "lease")
}

// StatsKey builds the stats key path (v3.0)
// Example: StatsKey("dc1") -> "/os/global/stats/dc1"
func StatsKey(idc string) string {
	return fmt.Sprintf("%s%s", KeyPrefixGlobalStats, idc)
}
