package nodes

import (
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	goredis "github.com/redis/go-redis/v9"
)

type Redis struct{}

type redisCredential struct {
	Host        string
	Port        int
	Password    string
	Database    int
	SSL         bool
	TLSInsecure bool
	PoolSize    int
}

type redisClientEntry struct {
	client   *goredis.Client
	lastUsed time.Time
}

type redisClientCache struct {
	mu      sync.Mutex
	clients map[string]*redisClientEntry
}

var redisClients = &redisClientCache{clients: map[string]*redisClientEntry{}}

func (Redis) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	credential := redisCredentialFromInput(in)
	client, err := redisClients.GetOrCreate(ctx, credential)
	if err != nil {
		return nil, err
	}
	items := firstInput(in.InputData)
	if len(items) == 0 {
		items = []dataplane.Item{{JSON: map[string]any{}}}
	}
	if redisSingleOutputOperation(in.Node.Parameters) {
		item, err := redisExecuteItem(ctx, client, in, items, 0)
		if err != nil {
			return nil, err
		}
		return dataplane.MainOutput([]dataplane.Item{item}), nil
	}
	output := make([]dataplane.Item, 0, len(items))
	for index := range items {
		item, err := redisExecuteItem(ctx, client, in, items, index)
		if err != nil {
			return nil, err
		}
		output = append(output, item)
	}
	return dataplane.MainOutput(output), nil
}

func (c *redisClientCache) GetOrCreate(ctx context.Context, credential redisCredential) (*goredis.Client, error) {
	hash := redisCredentialHash(credential)
	c.mu.Lock()
	if entry, ok := c.clients[hash]; ok {
		entry.lastUsed = time.Now().UTC()
		client := entry.client
		c.mu.Unlock()
		// Return the shared pool as-is; don't Close it here (other goroutines use it).
		return client, nil
	}
	c.mu.Unlock()
	client := buildRedisClient(credential)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	c.mu.Lock()
	c.clients[hash] = &redisClientEntry{client: client, lastUsed: time.Now().UTC()}
	c.mu.Unlock()
	return client, nil
}

func buildRedisClient(credential redisCredential) *goredis.Client {
	options := &goredis.Options{
		Addr:            fmt.Sprintf("%s:%d", credential.Host, credential.Port),
		Password:        credential.Password,
		DB:              credential.Database,
		DialTimeout:     10 * time.Second,
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		PoolSize:        credential.PoolSize,
		MinIdleConns:    1,
		ConnMaxIdleTime: 5 * time.Minute,
		ConnMaxLifetime: 30 * time.Minute,
	}
	if credential.SSL {
		options.TLSConfig = &tls.Config{InsecureSkipVerify: credential.TLSInsecure}
	}
	return goredis.NewClient(options)
}

func redisCredentialFromInput(in engine.ExecuteInput) redisCredential {
	credential := credentialByType(in.Credentials, "redis", "redisApi", "credentials")
	host := firstNonEmptyNode(stringParam(in.Node.Parameters, "host"), credentialString(credential, "host"), "localhost")
	return redisCredential{
		Host:        host,
		Port:        intParam(in.Node.Parameters, "port", redisCredentialInt(credential, "port", 6379)),
		Password:    firstNonEmptyNode(stringParam(in.Node.Parameters, "password"), credentialString(credential, "password")),
		Database:    intParam(in.Node.Parameters, "databaseNumber", redisCredentialInt(credential, "databaseNumber", redisCredentialInt(credential, "db", 0))),
		SSL:         boolParam(in.Node.Parameters, "ssl", redisCredentialBool(credential, "ssl", false)),
		TLSInsecure: boolParam(in.Node.Parameters, "tlsInsecure", redisCredentialBool(credential, "tlsInsecure", false)),
		PoolSize:    intParam(in.Node.Parameters, "poolSize", 10),
	}
}

func redisCredentialBool(credential map[string]any, key string, fallback bool) bool {
	if credential == nil {
		return fallback
	}
	return boolParam(credential, key, fallback)
}

func redisCredentialInt(credential map[string]any, key string, fallback int) int {
	if credential == nil {
		return fallback
	}
	return intParam(credential, key, fallback)
}

func redisCredentialHash(credential redisCredential) string {
	key := fmt.Sprintf("%s:%d:%s:%d:%t:%t:%d", credential.Host, credential.Port, credential.Password, credential.Database, credential.SSL, credential.TLSInsecure, credential.PoolSize)
	return fmt.Sprintf("%x", md5.Sum([]byte(key)))
}

func redisSingleOutputOperation(params map[string]any) bool {
	switch strings.ToLower(stringParam(params, "operation")) {
	case "info", "scan", "command":
		return true
	default:
		return false
	}
}

func redisExecuteItem(ctx context.Context, client *goredis.Client, in engine.ExecuteInput, items []dataplane.Item, index int) (dataplane.Item, error) {
	operation := strings.ToLower(stringParam(in.Node.Parameters, "operation"))
	key := redisString(in, items, index, "key")
	switch operation {
	case "info":
		value, err := client.Info(ctx).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return dataplane.Item{JSON: redisInfoToObject(value)}, nil
	case "", "get":
		value, err := redisGetValue(ctx, client, key, stringParam(in.Node.Parameters, "keyType"))
		if err != nil {
			return dataplane.Item{}, err
		}
		item := dataplane.Item{JSON: map[string]any{}, PairedItem: &dataplane.PairedItem{Item: index}}
		redisSetOutputValue(item.JSON, stringParamWithDefault(in.Node.Parameters, "propertyName", "propertyName"), value, redisDotNotation(in.Node.Parameters))
		return item, nil
	case "set":
		value := redisValue(in, items, index, "value")
		if err := redisSetOfficial(ctx, client, in.Node.Parameters, key, value); err != nil {
			return dataplane.Item{}, err
		}
		return cloneItem(items[index]), nil
	case "delete":
		deleted, err := client.Del(ctx, key).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		_ = deleted
		return cloneItem(items[index]), nil
	case "del":
		deleted, err := client.Del(ctx, key).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "deleted", deleted, "count", deleted), nil
	case "exists":
		exists, err := client.Exists(ctx, key).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "exists", exists > 0), nil
	case "expire":
		ok, err := client.Expire(ctx, key, redisTTL(in.Node.Parameters)).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "success", ok), nil
	case "ttl":
		ttl, err := client.TTL(ctx, key).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "ttl", redisTTLValue(ttl)), nil
	case "increment", "incr":
		value, err := client.Incr(ctx, key).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		if boolParam(in.Node.Parameters, "expire", false) {
			ttl := intParam(in.Node.Parameters, "ttl", -1)
			if ttl > 0 {
				if err := client.Expire(ctx, key, time.Duration(ttl)*time.Second).Err(); err != nil {
					return dataplane.Item{}, err
				}
			}
		}
		return redisItem(key, value), nil
	case "incrby":
		value, err := client.IncrBy(ctx, key, int64(redisNumber(in, items, index, "value", 1))).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "value", value), nil
	case "decrement", "decr":
		value, err := client.Decr(ctx, key).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "value", value), nil
	case "keys":
		pattern := redisPattern(in, items, index)
		keys, err := client.Keys(ctx, pattern).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		if !boolParam(in.Node.Parameters, "getValues", true) {
			return redisItem("keys", keys), nil
		}
		item := dataplane.Item{JSON: map[string]any{}, PairedItem: &dataplane.PairedItem{Item: index}}
		for _, keyName := range keys {
			value, err := redisGetValue(ctx, client, keyName, "automatic")
			if err != nil {
				return dataplane.Item{}, err
			}
			item.JSON[keyName] = value
		}
		return item, nil
	case "scan":
		pattern := redisPattern(in, items, index)
		count := int64(intParam(in.Node.Parameters, "count", 100))
		keys := make([]string, 0)
		var cursor uint64
		for {
			part, next, err := client.Scan(ctx, cursor, pattern, count).Result()
			if err != nil {
				return dataplane.Item{}, err
			}
			keys = append(keys, part...)
			cursor = next
			if cursor == 0 {
				break
			}
		}
		return redisItem("keys", keys, "count", len(keys)), nil
	case "hset":
		return redisHSet(ctx, client, in, items, index, key)
	case "hget":
		field := redisString(in, items, index, "field")
		value, err := client.HGet(ctx, key, field).Result()
		if err == goredis.Nil {
			return redisItem("key", key, "field", field, "value", nil, "found", false, "exists", false), nil
		}
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "field", field, "value", redisDecodeValue(in.Node.Parameters, value), "found", true, "exists", true), nil
	case "hgetall":
		values, err := client.HGetAll(ctx, key).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		result := map[string]any{}
		for field, value := range values {
			result[field] = redisDecodeValue(in.Node.Parameters, value)
		}
		return redisItem("key", key, "value", result), nil
	case "hdel":
		deleted, err := client.HDel(ctx, key, redisFields(in, items, index)...).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "deleted", deleted), nil
	case "hexists":
		field := redisString(in, items, index, "field")
		exists, err := client.HExists(ctx, key, field).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "field", field, "exists", exists), nil
	case "hkeys":
		fields, err := client.HKeys(ctx, key).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "fields", fields), nil
	case "hvals":
		values, err := client.HVals(ctx, key).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "value", redisDecodeSlice(in.Node.Parameters, values)), nil
	case "lpush":
		return redisListPush(ctx, client, in, items, index, key, true)
	case "rpush":
		return redisListPush(ctx, client, in, items, index, key, false)
	case "push":
		list := redisString(in, items, index, "list")
		message := redisString(in, items, index, "messageData")
		var err error
		if boolParam(in.Node.Parameters, "tail", false) {
			_, err = client.RPush(ctx, list, message).Result()
		} else {
			_, err = client.LPush(ctx, list, message).Result()
		}
		if err != nil {
			return dataplane.Item{}, err
		}
		return cloneItem(items[index]), nil
	case "lpop":
		return redisListPop(ctx, client, in.Node.Parameters, key, true)
	case "rpop":
		return redisListPop(ctx, client, in.Node.Parameters, key, false)
	case "pop":
		list := redisString(in, items, index, "list")
		propertyName := stringParamWithDefault(in.Node.Parameters, "propertyName", "propertyName")
		var value string
		var outputValue any
		var err error
		if boolParam(in.Node.Parameters, "tail", false) {
			value, err = client.RPop(ctx, list).Result()
		} else {
			value, err = client.LPop(ctx, list).Result()
		}
		if err == goredis.Nil {
			outputValue = nil
		} else if err != nil {
			return dataplane.Item{}, err
		} else {
			outputValue = redisDecodeJSONLike(value)
		}
		item := dataplane.Item{JSON: map[string]any{}, PairedItem: &dataplane.PairedItem{Item: index}}
		redisSetOutputValue(item.JSON, propertyName, outputValue, redisDotNotation(in.Node.Parameters))
		return item, nil
	case "lrange":
		values, err := client.LRange(ctx, key, int64(intParam(in.Node.Parameters, "start", 0)), int64(intParam(in.Node.Parameters, "stop", -1))).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "value", redisDecodeSlice(in.Node.Parameters, values)), nil
	case "llen":
		list := firstNonEmptyNode(redisString(in, items, index, "list"), key)
		length, err := client.LLen(ctx, list).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return dataplane.Item{JSON: map[string]any{list: length}, PairedItem: &dataplane.PairedItem{Item: index}}, nil
	case "sadd":
		member, err := redisEncodeValue(in.Node.Parameters, redisValue(in, items, index, "member", "value"))
		if err != nil {
			return dataplane.Item{}, err
		}
		count, err := client.SAdd(ctx, key, member).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "added", count), nil
	case "smembers":
		values, err := client.SMembers(ctx, key).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "value", redisDecodeSlice(in.Node.Parameters, values), "count", len(values)), nil
	case "sismember":
		member, err := redisEncodeValue(in.Node.Parameters, redisValue(in, items, index, "member", "value"))
		if err != nil {
			return dataplane.Item{}, err
		}
		ok, err := client.SIsMember(ctx, key, member).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "isMember", ok), nil
	case "srem":
		member, err := redisEncodeValue(in.Node.Parameters, redisValue(in, items, index, "member", "value"))
		if err != nil {
			return dataplane.Item{}, err
		}
		count, err := client.SRem(ctx, key, member).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "removed", count), nil
	case "scard":
		count, err := client.SCard(ctx, key).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "count", count), nil
	case "zadd":
		member, err := redisEncodeValue(in.Node.Parameters, redisValue(in, items, index, "member", "value"))
		if err != nil {
			return dataplane.Item{}, err
		}
		count, err := client.ZAdd(ctx, key, goredis.Z{Score: redisNumber(in, items, index, "score", 0), Member: member}).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "added", count), nil
	case "zrange":
		values, err := client.ZRange(ctx, key, int64(intParam(in.Node.Parameters, "start", 0)), int64(intParam(in.Node.Parameters, "stop", -1))).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "value", redisDecodeSlice(in.Node.Parameters, values)), nil
	case "zscore":
		member, err := redisEncodeValue(in.Node.Parameters, redisValue(in, items, index, "member", "value"))
		if err != nil {
			return dataplane.Item{}, err
		}
		score, err := client.ZScore(ctx, key, fmt.Sprint(member)).Result()
		if err == goredis.Nil {
			return redisItem("key", key, "score", nil), nil
		}
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "score", score), nil
	case "publish":
		channel := redisString(in, items, index, "channel")
		message := firstNonNil(redisValue(in, items, index, "messageData"), redisValue(in, items, index, "message", "value"))
		if err := client.Publish(ctx, channel, fmt.Sprint(message)).Err(); err != nil {
			return dataplane.Item{}, err
		}
		return cloneItem(items[index]), nil
	case "type":
		value, err := client.Type(ctx, key).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "type", value), nil
	case "rename":
		newKey := redisString(in, items, index, "newKey")
		if newKey == "" {
			newKey = fmt.Sprint(redisValue(in, items, index, "value"))
		}
		if err := client.Rename(ctx, key, newKey).Err(); err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("oldKey", key, "newKey", newKey), nil
	case "persist":
		ok, err := client.Persist(ctx, key).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "success", ok), nil
	case "command":
		result, err := client.Do(ctx, redisCommandArgs(in, items, index)...).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("value", result), nil
	default:
		return dataplane.Item{}, fmt.Errorf("unsupported redis operation %q", operation)
	}
}

func redisGetValue(ctx context.Context, client *goredis.Client, key string, keyType string) (any, error) {
	keyType = strings.ToLower(firstNonEmptyNode(keyType, "automatic"))
	if keyType == "automatic" {
		detected, err := client.Type(ctx, key).Result()
		if err != nil {
			return nil, err
		}
		keyType = detected
	}
	switch keyType {
	case "string":
		value, err := client.Get(ctx, key).Result()
		if err == goredis.Nil {
			return nil, nil
		}
		return value, err
	case "hash":
		return client.HGetAll(ctx, key).Result()
	case "list", "lists":
		return client.LRange(ctx, key, 0, -1).Result()
	case "set", "sets":
		return client.SMembers(ctx, key).Result()
	default:
		return nil, nil
	}
}

func redisSetOfficial(ctx context.Context, client *goredis.Client, params map[string]any, key string, value any) error {
	keyType := strings.ToLower(firstNonEmptyNode(stringParam(params, "keyType"), "automatic"))
	if keyType == "automatic" {
		switch value.(type) {
		case []any, []string:
			keyType = "list"
		case map[string]any:
			keyType = "hash"
		case string:
			keyType = "string"
		default:
			return fmt.Errorf("could not identify redis key type")
		}
	}
	switch keyType {
	case "string":
		if err := client.Set(ctx, key, fmt.Sprint(value), 0).Err(); err != nil {
			return err
		}
	case "hash":
		values, err := redisHashValues(value, boolParam(params, "valueIsJSON", true))
		if err != nil {
			return err
		}
		if len(values) > 0 {
			if err := client.HSet(ctx, key, values...).Err(); err != nil {
				return err
			}
		}
	case "list", "lists":
		values := redisListValues(value)
		for index, entry := range values {
			if err := client.LSet(ctx, key, int64(index), fmt.Sprint(entry)).Err(); err != nil {
				return err
			}
		}
	case "set", "sets":
		if err := client.SAdd(ctx, key, redisListValues(value)...).Err(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported redis key type %q", keyType)
	}
	if boolParam(params, "expire", false) {
		ttl := intParam(params, "ttl", -1)
		if ttl > 0 {
			return client.Expire(ctx, key, time.Duration(ttl)*time.Second).Err()
		}
	}
	return nil
}

func redisHashValues(value any, valueIsJSON bool) ([]any, error) {
	if !valueIsJSON {
		parts := strings.Fields(fmt.Sprint(value))
		values := make([]any, 0, len(parts))
		for _, part := range parts {
			values = append(values, part)
		}
		return values, nil
	}
	if text, ok := value.(string); ok {
		var decoded any
		if json.Unmarshal([]byte(text), &decoded) == nil {
			value = decoded
		}
	}
	object, ok := rawObject(value)
	if !ok {
		return []any{fmt.Sprint(value)}, nil
	}
	values := make([]any, 0, len(object)*2)
	for key, entry := range object {
		values = append(values, key, fmt.Sprint(entry))
	}
	return values, nil
}

func redisListValues(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []string:
		values := make([]any, 0, len(typed))
		for _, entry := range typed {
			values = append(values, entry)
		}
		return values
	case string:
		return []any{typed}
	default:
		return []any{fmt.Sprint(value)}
	}
}

func redisInfoToObject(data string) map[string]any {
	result := map[string]any{}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if strings.Contains(value, "=") {
			nested := map[string]any{}
			for _, pair := range strings.Split(value, ",") {
				nestedKey, nestedValue, ok := strings.Cut(pair, "=")
				if !ok {
					continue
				}
				nested[nestedKey] = redisParseInfoValue(nestedValue)
			}
			result[key] = nested
			continue
		}
		result[key] = redisParseInfoValue(value)
	}
	return result
}

func redisParseInfoValue(value string) any {
	value = strings.TrimSpace(value)
	if parsed, err := strconv.ParseFloat(value, 64); err == nil {
		return parsed
	}
	return value
}

func redisDotNotation(params map[string]any) bool {
	options := redisOptions(params)
	return boolParam(options, "dotNotation", true)
}

func redisSetOutputValue(target map[string]any, property string, value any, dotNotation bool) {
	if property == "" {
		property = "propertyName"
	}
	if !dotNotation || !strings.Contains(property, ".") {
		target[property] = value
		return
	}
	parts := strings.Split(property, ".")
	current := target
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

func redisDecodeJSONLike(value string) any {
	var decoded any
	if json.Unmarshal([]byte(value), &decoded) == nil {
		return decoded
	}
	return value
}

func stringParamWithDefault(params map[string]any, key string, fallback string) string {
	value := stringParam(params, key)
	if value == "" {
		return fallback
	}
	return value
}

func redisSet(ctx context.Context, client *goredis.Client, params map[string]any, key string, value any) (bool, error) {
	ttl := redisTTL(params)
	options := redisOptions(params)
	switch strings.ToLower(firstNonEmptyNode(stringParam(options, "setMode"), stringParam(options, "keepOnlySet"))) {
	case "nx", "true":
		return client.SetNX(ctx, key, value, ttl).Result()
	case "xx":
		return client.SetXX(ctx, key, value, ttl).Result()
	default:
		return true, client.Set(ctx, key, value, ttl).Err()
	}
}

func redisHSet(ctx context.Context, client *goredis.Client, in engine.ExecuteInput, items []dataplane.Item, index int, key string) (dataplane.Item, error) {
	field := redisString(in, items, index, "field")
	if field != "" {
		value, err := redisEncodeValue(in.Node.Parameters, redisValue(in, items, index, "value"))
		if err != nil {
			return dataplane.Item{}, err
		}
		count, err := client.HSet(ctx, key, field, value).Result()
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "field", field, "added", count, "created", count > 0), nil
	}
	args := make([]any, 0, len(items[index].JSON)*2)
	for field, value := range items[index].JSON {
		encoded, err := redisEncodeValue(in.Node.Parameters, value)
		if err != nil {
			return dataplane.Item{}, err
		}
		args = append(args, field, encoded)
	}
	count, err := client.HSet(ctx, key, args...).Result()
	if err != nil {
		return dataplane.Item{}, err
	}
	return redisItem("key", key, "added", count), nil
}

func redisListPush(ctx context.Context, client *goredis.Client, in engine.ExecuteInput, items []dataplane.Item, index int, key string, left bool) (dataplane.Item, error) {
	value, err := redisEncodeValue(in.Node.Parameters, redisValue(in, items, index, "value"))
	if err != nil {
		return dataplane.Item{}, err
	}
	var count int64
	if left {
		count, err = client.LPush(ctx, key, value).Result()
	} else {
		count, err = client.RPush(ctx, key, value).Result()
	}
	if err != nil {
		return dataplane.Item{}, err
	}
	return redisItem("key", key, "length", count), nil
}

func redisListPop(ctx context.Context, client *goredis.Client, params map[string]any, key string, left bool) (dataplane.Item, error) {
	count := intParam(params, "count", 0)
	if count > 0 {
		var values []string
		var err error
		if left {
			values, err = client.LPopCount(ctx, key, count).Result()
		} else {
			values, err = client.RPopCount(ctx, key, count).Result()
		}
		if err == goredis.Nil {
			return redisItem("key", key, "value", []any{}), nil
		}
		if err != nil {
			return dataplane.Item{}, err
		}
		return redisItem("key", key, "value", redisDecodeSlice(params, values)), nil
	}
	var value string
	var err error
	if left {
		value, err = client.LPop(ctx, key).Result()
	} else {
		value, err = client.RPop(ctx, key).Result()
	}
	if err == goredis.Nil {
		return redisItem("key", key, "value", nil), nil
	}
	if err != nil {
		return dataplane.Item{}, err
	}
	return redisItem("key", key, "value", redisDecodeValue(params, value)), nil
}

func redisString(in engine.ExecuteInput, items []dataplane.Item, index int, key string) string {
	value, ok := in.Node.Parameters[key]
	if !ok || value == nil {
		return ""
	}
	resolved := resolveValue(in, items, index, value)
	if resolved == nil {
		return ""
	}
	text := fmt.Sprint(resolved)
	if text == "<nil>" {
		return ""
	}
	return text
}

func redisValue(in engine.ExecuteInput, items []dataplane.Item, index int, keys ...string) any {
	for _, key := range keys {
		if value, ok := in.Node.Parameters[key]; ok {
			return resolveValue(in, items, index, value)
		}
	}
	if len(items) > index {
		for _, key := range keys {
			if value, ok := items[index].JSON[key]; ok {
				return value
			}
		}
		if len(keys) == 1 && keys[0] == "value" {
			return items[index].JSON
		}
	}
	return nil
}

func redisNumber(in engine.ExecuteInput, items []dataplane.Item, index int, key string, fallback float64) float64 {
	value := redisValue(in, items, index, key)
	if value == nil {
		return fallback
	}
	return number(value)
}

func redisTTL(params map[string]any) time.Duration {
	ttl := intParam(params, "ttl", intParam(params, "expire", 0))
	if ttl <= 0 {
		return 0
	}
	mode := strings.ToLower(firstNonEmptyNode(stringParam(redisOptions(params), "expireMode"), stringParam(params, "expireMode")))
	switch mode {
	case "milliseconds":
		return time.Duration(ttl) * time.Millisecond
	case "unixtimestamp":
		duration := time.Until(time.Unix(int64(ttl), 0))
		if duration < 0 {
			return time.Nanosecond
		}
		return duration
	default:
		return time.Duration(ttl) * time.Second
	}
}

func redisTTLValue(ttl time.Duration) any {
	switch ttl {
	case -2 * time.Second:
		return nil
	case -1 * time.Second:
		return int64(-1)
	default:
		return int64(ttl / time.Second)
	}
}

func redisEncodeValue(params map[string]any, value any) (any, error) {
	mode := strings.ToLower(firstNonEmptyNode(stringParam(redisOptions(params), "setValueAs"), stringParam(params, "setValueAs"), "auto"))
	if value == nil {
		return "", nil
	}
	switch mode {
	case "json":
		bytes, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return string(bytes), nil
	case "number":
		return fmt.Sprint(number(value)), nil
	case "string":
		return fmt.Sprint(value), nil
	default:
		switch typed := value.(type) {
		case string:
			return typed, nil
		case bool:
			if typed {
				return "true", nil
			}
			return "false", nil
		case int, int64, float64, float32:
			return fmt.Sprint(typed), nil
		default:
			bytes, err := json.Marshal(value)
			if err != nil {
				return fmt.Sprint(value), nil
			}
			return string(bytes), nil
		}
	}
}

func redisDecodeValue(params map[string]any, value string) any {
	mode := strings.ToLower(firstNonEmptyNode(stringParam(redisOptions(params), "getValueAs"), stringParam(params, "getValueAs"), "string"))
	switch mode {
	case "json":
		var decoded any
		if json.Unmarshal([]byte(value), &decoded) == nil {
			return decoded
		}
	case "number":
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			return parsed
		}
	case "auto":
		var decoded any
		if json.Unmarshal([]byte(value), &decoded) == nil {
			return decoded
		}
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
		if parsed, err := strconv.ParseFloat(value, 64); err == nil {
			return parsed
		}
		if value == "true" {
			return true
		}
		if value == "false" {
			return false
		}
	}
	return value
}

func redisDecodeSlice(params map[string]any, values []string) []any {
	decoded := make([]any, 0, len(values))
	for _, value := range values {
		decoded = append(decoded, redisDecodeValue(params, value))
	}
	return decoded
}

func redisCommandArgs(in engine.ExecuteInput, items []dataplane.Item, index int) []any {
	raw := in.Node.Parameters["args"]
	values, ok := raw.([]any)
	if !ok {
		command := strings.TrimSpace(redisString(in, items, index, "command"))
		if command == "" {
			return nil
		}
		parts := strings.Fields(command)
		args := make([]any, 0, len(parts))
		for _, part := range parts {
			args = append(args, part)
		}
		return args
	}
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, resolveValue(in, items, index, value))
	}
	return args
}

func redisPattern(in engine.ExecuteInput, items []dataplane.Item, index int) string {
	pattern := redisString(in, items, index, "pattern")
	if pattern == "" {
		return "*"
	}
	return pattern
}

func redisFields(in engine.ExecuteInput, items []dataplane.Item, index int) []string {
	if raw, ok := in.Node.Parameters["fields"]; ok {
		resolved := resolveValue(in, items, index, raw)
		switch typed := resolved.(type) {
		case []string:
			return typed
		case []any:
			fields := make([]string, 0, len(typed))
			for _, field := range typed {
				fields = append(fields, fmt.Sprint(field))
			}
			return fields
		case string:
			return splitCSV(typed)
		}
	}
	if field := redisString(in, items, index, "field"); field != "" {
		return []string{field}
	}
	return []string{}
}

func redisOptions(params map[string]any) map[string]any {
	if options, ok := rawObject(params["options"]); ok {
		return options
	}
	if additional, ok := rawObject(params["additionalFields"]); ok {
		return additional
	}
	return map[string]any{}
}

func redisItem(values ...any) dataplane.Item {
	item := dataplane.Item{JSON: map[string]any{}}
	for i := 0; i+1 < len(values); i += 2 {
		item.JSON[fmt.Sprint(values[i])] = values[i+1]
	}
	return item
}
