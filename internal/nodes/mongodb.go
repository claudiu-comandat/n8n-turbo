package nodes

import (
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/n8n-io/n8n-turbo/internal/dataplane"
	"github.com/n8n-io/n8n-turbo/internal/engine"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

type MongoDB struct{}

type mongoCredential struct {
	ConnectionString string
	Database         string
	AuthSource       string
	TLS              bool
	TLSInsecure      bool
	MaxPoolSize      uint64
	MinPoolSize      uint64
}

type mongoClientEntry struct {
	client   *mongo.Client
	lastUsed time.Time
}

type mongoClientCache struct {
	mu      sync.Mutex
	clients map[string]*mongoClientEntry
}

var mongoClients = &mongoClientCache{clients: map[string]*mongoClientEntry{}}

func (MongoDB) Execute(ctx context.Context, in engine.ExecuteInput) (dataplane.Output, error) {
	cred := mongoCredentialFromInput(in)
	if cred.Database == "" {
		return nil, fmt.Errorf("mongodb database is required")
	}
	collection := stringParam(in.Node.Parameters, "collection")
	if collection == "" {
		return nil, fmt.Errorf("mongodb collection is required")
	}
	client, err := mongoClients.GetOrCreate(ctx, cred)
	if err != nil {
		return nil, err
	}
	coll := client.Database(cred.Database).Collection(collection)
	switch strings.ToLower(stringParam(in.Node.Parameters, "operation")) {
	case "", "find":
		return mongoFind(ctx, coll, in)
	case "findone":
		return mongoFindOne(ctx, coll, in)
	case "insertone":
		return mongoInsertOne(ctx, coll, in)
	case "insertmany":
		return mongoInsertMany(ctx, coll, in)
	case "updateone":
		return mongoUpdate(ctx, coll, in, false)
	case "updatemany":
		return mongoUpdate(ctx, coll, in, true)
	case "deleteone":
		return mongoDelete(ctx, coll, in, false)
	case "deletemany":
		return mongoDelete(ctx, coll, in, true)
	case "aggregate":
		return mongoAggregate(ctx, coll, in)
	case "countdocuments", "count":
		return mongoCount(ctx, coll, in)
	case "findoneandupdate":
		return mongoFindOneAndUpdate(ctx, coll, in)
	case "findoneanddelete":
		return mongoFindOneAndDelete(ctx, coll, in)
	default:
		return nil, fmt.Errorf("unsupported mongodb operation %q", stringParam(in.Node.Parameters, "operation"))
	}
}

func (c *mongoClientCache) GetOrCreate(ctx context.Context, cred mongoCredential) (*mongo.Client, error) {
	hash := mongoCredentialHash(cred)
	c.mu.Lock()
	if entry, ok := c.clients[hash]; ok {
		entry.lastUsed = time.Now().UTC()
		client := entry.client
		c.mu.Unlock()
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := client.Ping(pingCtx, readpref.Primary()); err == nil {
			return client, nil
		}
		_ = client.Disconnect(ctx)
		c.mu.Lock()
		delete(c.clients, hash)
		c.mu.Unlock()
	} else {
		c.mu.Unlock()
	}
	client, err := buildMongoClient(ctx, cred)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.clients[hash] = &mongoClientEntry{client: client, lastUsed: time.Now().UTC()}
	c.mu.Unlock()
	return client, nil
}

func buildMongoClient(ctx context.Context, cred mongoCredential) (*mongo.Client, error) {
	uri := mongoURIWithAuthSource(cred.ConnectionString, cred.AuthSource)
	clientOptions := options.Client().
		ApplyURI(uri).
		SetServerAPIOptions(options.ServerAPI(options.ServerAPIVersion1)).
		SetConnectTimeout(30 * time.Second).
		SetServerSelectionTimeout(10 * time.Second).
		SetMaxConnIdleTime(5 * time.Minute)
	if cred.MaxPoolSize > 0 {
		clientOptions.SetMaxPoolSize(cred.MaxPoolSize)
	}
	if cred.MinPoolSize > 0 {
		clientOptions.SetMinPoolSize(cred.MinPoolSize)
	}
	if cred.TLS || cred.TLSInsecure {
		clientOptions.SetTLSConfig(&tls.Config{InsecureSkipVerify: cred.TLSInsecure})
	}
	client, err := mongo.Connect(clientOptions)
	if err != nil {
		return nil, fmt.Errorf("mongodb client: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("mongodb ping: %w", err)
	}
	return client, nil
}

func mongoCredentialFromInput(in engine.ExecuteInput) mongoCredential {
	credential := credentialByType(in.Credentials, "mongoDb", "mongodb", "mongoDB", "credentials")
	connectionString := firstNonEmptyNode(stringParam(in.Node.Parameters, "connectionString", "uri"), credentialString(credential, "connectionString", "uri"), "mongodb://localhost:27017")
	database := firstNonEmptyNode(stringParam(in.Node.Parameters, "database", "db"), credentialString(credential, "database", "db"))
	authSource := firstNonEmptyNode(stringParam(in.Node.Parameters, "authenticationDatabase", "authSource"), credentialString(credential, "authenticationDatabase", "authSource"))
	return mongoCredential{
		ConnectionString: connectionString,
		Database:         database,
		AuthSource:       authSource,
		TLS:              boolParam(in.Node.Parameters, "tls", mongoCredentialBool(credential, "tls", false)),
		TLSInsecure:      boolParam(in.Node.Parameters, "tlsInsecure", mongoCredentialBool(credential, "tlsInsecure", false)),
		MaxPoolSize:      uint64(intParam(in.Node.Parameters, "maxPoolSize", 20)),
		MinPoolSize:      uint64(intParam(in.Node.Parameters, "minPoolSize", 1)),
	}
}

func mongoCredentialBool(credential map[string]any, key string, fallback bool) bool {
	if credential == nil {
		return fallback
	}
	return boolParam(credential, key, fallback)
}

func mongoCredentialHash(cred mongoCredential) string {
	key := fmt.Sprintf("%s:%s:%s:%t:%t:%d:%d", cred.ConnectionString, cred.Database, cred.AuthSource, cred.TLS, cred.TLSInsecure, cred.MaxPoolSize, cred.MinPoolSize)
	return fmt.Sprintf("%x", md5.Sum([]byte(key)))
}

func mongoURIWithAuthSource(raw string, authSource string) string {
	if strings.TrimSpace(authSource) == "" {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := parsed.Query()
	if query.Get("authSource") != "" {
		return raw
	}
	query.Set("authSource", authSource)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func mongoFind(ctx context.Context, coll *mongo.Collection, in engine.ExecuteInput) (dataplane.Output, error) {
	items := firstInput(in.InputData)
	findOptions := options.Find()
	if limit := intParam(in.Node.Parameters, "limit", 0); limit > 0 {
		findOptions.SetLimit(int64(limit))
	}
	if skip := intParam(in.Node.Parameters, "skip", intParam(in.Node.Parameters, "offset", 0)); skip > 0 {
		findOptions.SetSkip(int64(skip))
	}
	if projection := mongoResolvedDocument(in, items, 0, in.Node.Parameters["projection"]); len(projection) > 0 {
		findOptions.SetProjection(projection)
	}
	if sort := mongoResolvedDocument(in, items, 0, in.Node.Parameters["sort"]); len(sort) > 0 {
		findOptions.SetSort(sort)
	}
	cursor, err := coll.Find(ctx, mongoResolvedDocument(in, items, 0, firstPresent(in.Node.Parameters, "query", "filter")), findOptions)
	if err != nil {
		return nil, wrapMongoError("find", err)
	}
	defer cursor.Close(ctx)
	itemsOut, err := mongoCursorItems(ctx, cursor)
	if err != nil {
		return nil, err
	}
	return dataplane.MainOutput(itemsOut), nil
}

func mongoFindOne(ctx context.Context, coll *mongo.Collection, in engine.ExecuteInput) (dataplane.Output, error) {
	items := firstInput(in.InputData)
	findOptions := options.FindOne()
	if projection := mongoResolvedDocument(in, items, 0, in.Node.Parameters["projection"]); len(projection) > 0 {
		findOptions.SetProjection(projection)
	}
	if sort := mongoResolvedDocument(in, items, 0, in.Node.Parameters["sort"]); len(sort) > 0 {
		findOptions.SetSort(sort)
	}
	if skip := intParam(in.Node.Parameters, "skip", intParam(in.Node.Parameters, "offset", 0)); skip > 0 {
		findOptions.SetSkip(int64(skip))
	}
	var document bson.M
	err := coll.FindOne(ctx, mongoResolvedDocument(in, items, 0, firstPresent(in.Node.Parameters, "query", "filter")), findOptions).Decode(&document)
	if err == mongo.ErrNoDocuments {
		return dataplane.MainOutput([]dataplane.Item{}), nil
	}
	if err != nil {
		return nil, wrapMongoError("findOne", err)
	}
	return dataplane.MainOutput([]dataplane.Item{{JSON: mongoJSON(document)}}), nil
}

func mongoInsertOne(ctx context.Context, coll *mongo.Collection, in engine.ExecuteInput) (dataplane.Output, error) {
	items := firstInput(in.InputData)
	document := mongoInputDocument(in, items, 0)
	result, err := coll.InsertOne(ctx, document)
	if err != nil {
		return nil, wrapMongoError("insertOne", err)
	}
	output := mongoJSON(document)
	insertedID := mongoJSONValue(result.InsertedID)
	output["_id"] = insertedID
	output["insertedId"] = insertedID
	return dataplane.MainOutput([]dataplane.Item{{JSON: output}}), nil
}

func mongoInsertMany(ctx context.Context, coll *mongo.Collection, in engine.ExecuteInput) (dataplane.Output, error) {
	items := firstInput(in.InputData)
	documents := mongoInputDocuments(in, items)
	if len(documents) == 0 {
		return dataplane.MainOutput([]dataplane.Item{}), nil
	}
	rawDocuments := make([]any, 0, len(documents))
	for _, document := range documents {
		rawDocuments = append(rawDocuments, document)
	}
	ordered := boolParam(mongoOptions(in.Node.Parameters), "ordered", true)
	result, err := coll.InsertMany(ctx, rawDocuments, options.InsertMany().SetOrdered(ordered))
	if err != nil {
		return nil, wrapMongoError("insertMany", err)
	}
	output := make([]dataplane.Item, 0, len(documents))
	for index, document := range documents {
		row := mongoJSON(document)
		if index < len(result.InsertedIDs) {
			insertedID := mongoJSONValue(result.InsertedIDs[index])
			row["_id"] = insertedID
			row["insertedId"] = insertedID
		}
		output = append(output, dataplane.Item{JSON: row})
	}
	return dataplane.MainOutput(output), nil
}

func mongoUpdate(ctx context.Context, coll *mongo.Collection, in engine.ExecuteInput, many bool) (dataplane.Output, error) {
	items := firstInput(in.InputData)
	filter := mongoResolvedDocument(in, items, 0, firstPresent(in.Node.Parameters, "filter", "query"))
	update := mongoUpdateDocument(resolveValue(in, items, 0, in.Node.Parameters["update"]))
	upsert := boolParam(in.Node.Parameters, "upsert", false)
	if many {
		result, err := coll.UpdateMany(ctx, filter, update, options.UpdateMany().SetUpsert(upsert))
		if err != nil {
			return nil, wrapMongoError("updateMany", err)
		}
		return dataplane.MainOutput([]dataplane.Item{mongoUpdateResult(result.MatchedCount, result.ModifiedCount, result.UpsertedCount, result.UpsertedID)}), nil
	}
	result, err := coll.UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(upsert))
	if err != nil {
		return nil, wrapMongoError("updateOne", err)
	}
	return dataplane.MainOutput([]dataplane.Item{mongoUpdateResult(result.MatchedCount, result.ModifiedCount, result.UpsertedCount, result.UpsertedID)}), nil
}

func mongoDelete(ctx context.Context, coll *mongo.Collection, in engine.ExecuteInput, many bool) (dataplane.Output, error) {
	items := firstInput(in.InputData)
	filter := mongoResolvedDocument(in, items, 0, firstPresent(in.Node.Parameters, "filter", "query"))
	if many {
		result, err := coll.DeleteMany(ctx, filter)
		if err != nil {
			return nil, wrapMongoError("deleteMany", err)
		}
		return dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"deletedCount": result.DeletedCount}}}), nil
	}
	result, err := coll.DeleteOne(ctx, filter)
	if err != nil {
		return nil, wrapMongoError("deleteOne", err)
	}
	return dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"deletedCount": result.DeletedCount}}}), nil
}

func mongoAggregate(ctx context.Context, coll *mongo.Collection, in engine.ExecuteInput) (dataplane.Output, error) {
	items := firstInput(in.InputData)
	aggregateOptions := options.Aggregate()
	if boolParam(mongoOptions(in.Node.Parameters), "allowDiskUse", false) {
		aggregateOptions.SetAllowDiskUse(true)
	}
	cursor, err := coll.Aggregate(ctx, mongoResolvedPipeline(in, items, 0, in.Node.Parameters["pipeline"]), aggregateOptions)
	if err != nil {
		return nil, wrapMongoError("aggregate", err)
	}
	defer cursor.Close(ctx)
	itemsOut, err := mongoCursorItems(ctx, cursor)
	if err != nil {
		return nil, err
	}
	return dataplane.MainOutput(itemsOut), nil
}

func mongoCount(ctx context.Context, coll *mongo.Collection, in engine.ExecuteInput) (dataplane.Output, error) {
	items := firstInput(in.InputData)
	countOptions := options.Count()
	if limit := intParam(in.Node.Parameters, "limit", 0); limit > 0 {
		countOptions.SetLimit(int64(limit))
	}
	if skip := intParam(in.Node.Parameters, "skip", intParam(in.Node.Parameters, "offset", 0)); skip > 0 {
		countOptions.SetSkip(int64(skip))
	}
	count, err := coll.CountDocuments(ctx, mongoResolvedDocument(in, items, 0, firstPresent(in.Node.Parameters, "query", "filter")), countOptions)
	if err != nil {
		return nil, wrapMongoError("countDocuments", err)
	}
	return dataplane.MainOutput([]dataplane.Item{{JSON: map[string]any{"count": count}}}), nil
}

func mongoFindOneAndUpdate(ctx context.Context, coll *mongo.Collection, in engine.ExecuteInput) (dataplane.Output, error) {
	items := firstInput(in.InputData)
	opts := options.FindOneAndUpdate().SetUpsert(boolParam(in.Node.Parameters, "upsert", false))
	if projection := mongoResolvedDocument(in, items, 0, in.Node.Parameters["projection"]); len(projection) > 0 {
		opts.SetProjection(projection)
	}
	if sort := mongoResolvedDocument(in, items, 0, in.Node.Parameters["sort"]); len(sort) > 0 {
		opts.SetSort(sort)
	}
	if strings.EqualFold(stringParam(mongoOptions(in.Node.Parameters), "returnDocuments"), "updated") || strings.EqualFold(stringParam(mongoOptions(in.Node.Parameters), "returnDocument"), "after") {
		opts.SetReturnDocument(options.After)
	} else {
		opts.SetReturnDocument(options.Before)
	}
	var document bson.M
	err := coll.FindOneAndUpdate(ctx, mongoResolvedDocument(in, items, 0, firstPresent(in.Node.Parameters, "filter", "query")), mongoUpdateDocument(resolveValue(in, items, 0, in.Node.Parameters["update"])), opts).Decode(&document)
	if err == mongo.ErrNoDocuments {
		return dataplane.MainOutput([]dataplane.Item{}), nil
	}
	if err != nil {
		return nil, wrapMongoError("findOneAndUpdate", err)
	}
	return dataplane.MainOutput([]dataplane.Item{{JSON: mongoJSON(document)}}), nil
}

func mongoFindOneAndDelete(ctx context.Context, coll *mongo.Collection, in engine.ExecuteInput) (dataplane.Output, error) {
	items := firstInput(in.InputData)
	opts := options.FindOneAndDelete()
	if projection := mongoResolvedDocument(in, items, 0, in.Node.Parameters["projection"]); len(projection) > 0 {
		opts.SetProjection(projection)
	}
	if sort := mongoResolvedDocument(in, items, 0, in.Node.Parameters["sort"]); len(sort) > 0 {
		opts.SetSort(sort)
	}
	var document bson.M
	err := coll.FindOneAndDelete(ctx, mongoResolvedDocument(in, items, 0, firstPresent(in.Node.Parameters, "filter", "query")), opts).Decode(&document)
	if err == mongo.ErrNoDocuments {
		return dataplane.MainOutput([]dataplane.Item{}), nil
	}
	if err != nil {
		return nil, wrapMongoError("findOneAndDelete", err)
	}
	return dataplane.MainOutput([]dataplane.Item{{JSON: mongoJSON(document)}}), nil
}

func mongoCursorItems(ctx context.Context, cursor *mongo.Cursor) ([]dataplane.Item, error) {
	items := make([]dataplane.Item, 0)
	for cursor.Next(ctx) {
		item, err := mongoCursorItem(cursor)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := cursor.Err(); err != nil {
		return nil, wrapMongoError("cursor", err)
	}
	return items, nil
}

func mongoCursorItem(cursor *mongo.Cursor) (dataplane.Item, error) {
	var document bson.M
	if err := cursor.Decode(&document); err != nil {
		return dataplane.Item{}, err
	}
	return dataplane.Item{JSON: mongoJSON(document)}, nil
}

func mongoInputDocument(in engine.ExecuteInput, items []dataplane.Item, index int) bson.M {
	if _, ok := in.Node.Parameters["document"]; ok {
		return mongoResolvedDocument(in, items, index, in.Node.Parameters["document"])
	}
	if raw, ok := in.Node.Parameters["documents"]; ok {
		values := mongoArray(raw)
		if len(values) > index {
			return mongoResolvedDocument(in, items, index, values[index])
		}
	}
	if len(items) > index {
		return mongoDocument(items[index].JSON)
	}
	return bson.M{}
}

func mongoInputDocuments(in engine.ExecuteInput, items []dataplane.Item) []bson.M {
	if raw, ok := in.Node.Parameters["documents"]; ok {
		values := mongoArray(raw)
		documents := make([]bson.M, 0, len(values))
		for index, value := range values {
			documents = append(documents, mongoResolvedDocument(in, items, index, value))
		}
		return documents
	}
	if _, ok := in.Node.Parameters["document"]; ok {
		return []bson.M{mongoInputDocument(in, items, 0)}
	}
	documents := make([]bson.M, 0, len(items))
	for index := range items {
		documents = append(documents, mongoInputDocument(in, items, index))
	}
	return documents
}

func mongoResolvedDocument(in engine.ExecuteInput, items []dataplane.Item, index int, value any) bson.M {
	return mongoDocument(resolveValue(in, items, index, value))
}

func mongoResolvedPipeline(in engine.ExecuteInput, items []dataplane.Item, index int, value any) []bson.M {
	return mongoPipeline(resolveValue(in, items, index, value))
}

func mongoDocument(value any) bson.M {
	switch typed := value.(type) {
	case nil:
		return bson.M{}
	case bson.M:
		return normalizeMongoDocument(typed)
	case map[string]any:
		return normalizeMongoDocument(bson.M(typed))
	case map[string]string:
		result := bson.M{}
		for key, value := range typed {
			result[key] = value
		}
		return result
	case string:
		if strings.TrimSpace(typed) == "" {
			return bson.M{}
		}
		var decoded map[string]any
		if json.Unmarshal([]byte(typed), &decoded) == nil {
			return normalizeMongoDocument(bson.M(decoded))
		}
		return bson.M{}
	default:
		bytes, err := json.Marshal(value)
		if err != nil {
			return bson.M{}
		}
		var decoded map[string]any
		if json.Unmarshal(bytes, &decoded) != nil {
			return bson.M{}
		}
		return normalizeMongoDocument(bson.M(decoded))
	}
}

func normalizeMongoDocument(document bson.M) bson.M {
	result := bson.M{}
	for key, value := range document {
		if key == "_id" {
			if text, ok := value.(string); ok && len(text) == 24 && mongoIsHex(text) {
				if id, err := bson.ObjectIDFromHex(text); err == nil {
					result[key] = id
					continue
				}
			}
		}
		result[key] = mongoBSONValue(value)
	}
	return result
}

func mongoBSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		if converted, ok := mongoExtendedDocumentValue(bson.M(typed)); ok {
			return converted
		}
		return normalizeMongoDocument(bson.M(typed))
	case bson.M:
		if converted, ok := mongoExtendedDocumentValue(typed); ok {
			return converted
		}
		return normalizeMongoDocument(typed)
	case []any:
		array := make(bson.A, 0, len(typed))
		for _, value := range typed {
			array = append(array, mongoBSONValue(value))
		}
		return array
	case []map[string]any:
		array := make(bson.A, 0, len(typed))
		for _, value := range typed {
			array = append(array, mongoBSONValue(value))
		}
		return array
	case string:
		return typed
	case float64:
		if typed == float64(int64(typed)) {
			return int64(typed)
		}
		return typed
	default:
		return typed
	}
}

func mongoExtendedDocumentValue(document bson.M) (any, bool) {
	if len(document) != 1 {
		return nil, false
	}
	if raw, ok := document["$oid"]; ok {
		text := fmt.Sprint(raw)
		id, err := bson.ObjectIDFromHex(text)
		if err == nil {
			return id, true
		}
	}
	if raw, ok := document["$date"]; ok {
		if typed, ok := raw.(time.Time); ok {
			return bson.NewDateTimeFromTime(typed), true
		}
		parsed, err := time.Parse(time.RFC3339, fmt.Sprint(raw))
		if err == nil {
			return bson.NewDateTimeFromTime(parsed), true
		}
	}
	return nil, false
}

func mongoUpdateDocument(value any) bson.M {
	document := mongoDocument(value)
	for key := range document {
		if strings.HasPrefix(key, "$") {
			return document
		}
	}
	return bson.M{"$set": document}
}

func mongoPipeline(value any) []bson.M {
	switch typed := value.(type) {
	case []bson.M:
		return typed
	case []any:
		result := make([]bson.M, 0, len(typed))
		for _, stage := range typed {
			result = append(result, mongoDocument(stage))
		}
		return result
	case []map[string]any:
		result := make([]bson.M, 0, len(typed))
		for _, stage := range typed {
			result = append(result, mongoDocument(stage))
		}
		return result
	case string:
		var decoded []map[string]any
		if json.Unmarshal([]byte(typed), &decoded) == nil {
			result := make([]bson.M, 0, len(decoded))
			for _, stage := range decoded {
				result = append(result, mongoDocument(stage))
			}
			return result
		}
	}
	return []bson.M{}
}

func mongoArray(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		result := make([]any, 0, len(typed))
		for _, value := range typed {
			result = append(result, value)
		}
		return result
	case string:
		var decoded []any
		if json.Unmarshal([]byte(typed), &decoded) == nil {
			return decoded
		}
	}
	return []any{}
}

func mongoOptions(params map[string]any) map[string]any {
	if options, ok := rawObject(params["options"]); ok {
		return options
	}
	if additional, ok := rawObject(params["additionalFields"]); ok {
		return additional
	}
	return map[string]any{}
}

func mongoIsHex(value string) bool {
	for _, char := range value {
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F') {
			continue
		}
		return false
	}
	return true
}

func mongoJSON(document bson.M) map[string]any {
	result := map[string]any{}
	for key, value := range document {
		result[key] = mongoJSONValue(value)
	}
	return result
}

func mongoJSONValue(value any) any {
	switch typed := value.(type) {
	case bson.ObjectID:
		return typed.Hex()
	case bson.DateTime:
		return typed.Time().UTC().Format(time.RFC3339)
	case bson.Timestamp:
		return map[string]any{"t": typed.T, "i": typed.I}
	case bson.Binary:
		return hex.EncodeToString(typed.Data)
	case bson.Regex:
		return "/" + typed.Pattern + "/" + typed.Options
	case bson.Decimal128:
		return typed.String()
	case bson.M:
		return mongoJSON(typed)
	case bson.D:
		result := map[string]any{}
		for _, element := range typed {
			result[element.Key] = mongoJSONValue(element.Value)
		}
		return result
	case bson.A:
		result := make([]any, 0, len(typed))
		for _, value := range typed {
			result = append(result, mongoJSONValue(value))
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, value := range typed {
			result = append(result, mongoJSONValue(value))
		}
		return result
	case map[string]any:
		result := map[string]any{}
		for key, value := range typed {
			result[key] = mongoJSONValue(value)
		}
		return result
	case time.Time:
		return typed.UTC().Format(time.RFC3339)
	default:
		return typed
	}
}

func mongoUpdateResult(matched int64, modified int64, upserted int64, upsertedID any) dataplane.Item {
	return dataplane.Item{JSON: map[string]any{"matchedCount": matched, "modifiedCount": modified, "upsertedCount": upserted, "upsertedId": mongoJSONValue(upsertedID)}}
}

func wrapMongoError(operation string, err error) error {
	if err == nil {
		return nil
	}
	switch {
	case mongo.IsDuplicateKeyError(err):
		return fmt.Errorf("mongodb %s duplicate key: %w", operation, err)
	case mongo.IsTimeout(err):
		return fmt.Errorf("mongodb %s timeout: %w", operation, err)
	case mongo.IsNetworkError(err):
		return fmt.Errorf("mongodb %s network: %w", operation, err)
	default:
		return fmt.Errorf("mongodb %s: %w", operation, err)
	}
}
