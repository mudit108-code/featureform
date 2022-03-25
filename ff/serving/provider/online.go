package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

func init() {
	if err := RegisterFactory(LocalOnline, localOnlineStoreFactory); err != nil {
		panic(err)
	}
	if err := RegisterFactory(RedisOnline, redisOnlineStoreFactory); err != nil {
		panic(err)
	}
}

const LocalOnline Type = "LOCAL_ONLINE"
const RedisOnline Type = "REDIS_ONLINE"

var ctx = context.Background()

type OnlineStore interface {
	GetTable(feature, variant string) (OnlineStoreTable, error)
	CreateTable(feature, variant string) (OnlineStoreTable, error)
}

type OnlineStoreTable interface {
	Set(entity string, value interface{}) error
	Get(entity string) (interface{}, error)
}

type TableNotFound struct {
	Feature, Variant string
}

func (err *TableNotFound) Error() string {
	return fmt.Sprintf("Feature %s Variant %s not found.", err.Feature, err.Variant)
}

type TableAlreadyExists struct {
	Feature, Variant string
}

func (err *TableAlreadyExists) Error() string {
	return fmt.Sprintf("Feature %s Variant %s already exists.", err.Feature, err.Variant)
}

type EntityNotFound struct {
	Entity string
}

func (err *EntityNotFound) Error() string {
	return fmt.Sprintf("Entity %s not found.", err.Entity)
}

type tableKey struct {
	Prefix, Feature, Variant string
}

func (t tableKey) String() string {
	marshalled, _ := json.Marshal(t)
	return string(marshalled)
}

func localOnlineStoreFactory(SerializedConfig) (Provider, error) {
	return NewLocalOnlineStore(), nil
}

type localOnlineStore struct {
	tables map[tableKey]localOnlineTable
}

type redisOnlineStore struct {
	client *redis.Client
	prefix string
}

func NewLocalOnlineStore() *localOnlineStore {
	return &localOnlineStore{
		tables: make(map[tableKey]localOnlineTable),
	}
}

func redisOnlineStoreFactory(serialized SerializedConfig) (Provider, error) {
	redisConfig := &RedisConfig{}
	if err := redisConfig.Deserialize(serialized); err != nil {
		return nil, err
	}
	if redisConfig.Prefix == "" {
		redisConfig.Prefix = fmt.Sprintf("%s__", uuid.NewString())
	}
	return NewRedisOnlineStore(redisConfig), nil
}

func NewRedisOnlineStore(options *RedisConfig) *redisOnlineStore {
	redisOptions := &redis.Options{
		Addr: options.Addr,
	}
	redisClient := redis.NewClient(redisOptions)
	return &redisOnlineStore{client: redisClient, prefix: options.Prefix}
}

func (store *localOnlineStore) AsOnlineStore() (OnlineStore, error) {
	return store, nil
}

func (store *redisOnlineStore) AsOnlineStore() (OnlineStore, error) {
	return store, nil
}

func (store *localOnlineStore) GetTable(feature, variant string) (OnlineStoreTable, error) {
	table, has := store.tables[tableKey{Feature: feature, Variant: variant}]
	if !has {
		return nil, &TableNotFound{feature, variant}
	}
	return table, nil
}

func (store *localOnlineStore) CreateTable(feature, variant string) (OnlineStoreTable, error) {
	key := tableKey{Feature: feature, Variant: variant}
	if _, has := store.tables[key]; has {
		return nil, &TableAlreadyExists{feature, variant}
	}
	table := make(localOnlineTable)
	store.tables[key] = table
	return table, nil
}

func (store *redisOnlineStore) GetTable(feature, variant string) (OnlineStoreTable, error) {
	key := tableKey{store.prefix, feature, variant}
	exists, err := store.client.HExists(ctx, fmt.Sprintf("%s__tables", store.prefix), key.String()).Result()
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, &TableNotFound{feature, variant}
	}
	table := &redisOnlineTable{client: store.client, key: key}
	return table, nil
}

func (store *redisOnlineStore) CreateTable(feature, variant string) (OnlineStoreTable, error) {
	key := tableKey{store.prefix, feature, variant}
	exists, err := store.client.HExists(ctx, fmt.Sprintf("%s__tables", store.prefix), key.String()).Result()
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, &TableAlreadyExists{feature, variant}
	}
	if err := store.client.HSet(ctx, fmt.Sprintf("%s__tables", store.prefix), key.String(), 1).Err(); err != nil {
		return nil, err
	}
	table := &redisOnlineTable{client: store.client, key: key}
	return table, nil
}

type localOnlineTable map[string]interface{}

type redisOnlineTable struct {
	client *redis.Client
	key    tableKey
}

func (table localOnlineTable) Set(entity string, value interface{}) error {
	table[entity] = value
	return nil
}

func (table localOnlineTable) Get(entity string) (interface{}, error) {
	val, has := table[entity]
	if !has {
		return nil, &EntityNotFound{entity}
	}
	return val, nil
}

func (table redisOnlineTable) Set(entity string, value interface{}) error {
	val := table.client.HSet(ctx, table.key.String(), entity, value)
	if val.Err() != nil {
		return val.Err()
	}
	return nil
}

func (table redisOnlineTable) Get(entity string) (interface{}, error) {
	val := table.client.HMGet(ctx, table.key.String(), entity)
	result, err := val.Result()
	if err != nil {
		return nil, err
	}
	if result[0] == nil {
		return nil, &EntityNotFound{entity}
	}
	return result[0], nil
}