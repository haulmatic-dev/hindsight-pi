# Hindsight Go Client Analysis: Official vs Custom Implementation

## Overview

This document compares our custom Hindsight Go client with the official Vectorize client to identify improvements we can borrow.

## Official Client Structure

**Repository**: `github.com/vectorize-io/hindsight/hindsight-clients/go`
**Generator**: OpenAPI Generator 7.10.0
**API Version**: 0.4.19

### Key Files:
- `api_memory.go` - Memory operations (Retain, Recall, Reflect)
- `model_retain_request.go` - Strongly-typed request structs
- `model_memory_item.go` - Memory item structure
- `client.go` - HTTP client with configuration
- `configuration.go` - Server configuration

## Comparison Analysis

### 1. Request/Response Types

**Official Client**:
```go
type RetainRequest struct {
    Items        []MemoryItem `json:"items"`
    Async        *bool        `json:"async,omitempty"`
    DocumentTags []string     `json:"document_tags,omitempty"`
}

type MemoryItem struct {
    Content    string                 `json:"content"`
    Context    *string                `json:"context,omitempty"`
    DocumentID *string                `json:"document_id,omitempty"`
    Metadata   map[string]interface{} `json:"metadata,omitempty"`
    Tags       []string               `json:"tags,omitempty"`
    Timestamp  *Timestamp             `json:"timestamp,omitempty"`
}
```

**Our Implementation**:
```go
// Uses map[string]interface{}
return map[string]interface{}{
    "items": []map[string]interface{}{...},
    "async": true,
}
```

**Recommendation**: 
- ✅ Keep our simple approach - it's more flexible and easier to maintain
- The official client's strongly-typed structs are verbose for our use case

### 2. API Pattern

**Official Client** (Fluent API):
```go
client.MemoryAPI.RetainMemories(ctx, bankId).
    RetainRequest(retainReq).
    Execute()
```

**Our Implementation** (Direct):
```go
client.Retain(memory)
```

**Recommendation**:
- ✅ Our direct approach is simpler and more Go-idiomatic
- The fluent API adds unnecessary complexity

### 3. Context Support

**Official Client**:
- All methods accept `context.Context` as first parameter
- Enables cancellation and timeout control per-request
- Proper deadline handling

**Our Implementation**:
```go
ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
defer cancel()
```

**Recommendation**:
- 🔄 **BORROW THIS**: Pass context from caller for better control
- Update `Retain()` signature: `Retain(ctx context.Context, memory *ExecutionMemory)`
- Update `Search()` signature: `Search(ctx context.Context, query, beadType string, limit int)`

### 4. Error Handling

**Official Client**:
```go
type GenericOpenAPIError struct {
    body  []byte
    error string
    model interface{}
}

func (e GenericOpenAPIError) Error() string {
    return e.error
}

func (e GenericOpenAPIError) Body() []byte {
    return e.body
}

func (e GenericOpenAPIError) Model() interface{} {
    return e.model
}
```

**Our Implementation**:
```go
type ClientError struct {
    Op       Operation
    Message  string
    Endpoint string
    Status   int
    Cause    error
    Context  *ErrorContext
}
```

**Recommendation**:
- ✅ Our error type is better - includes operation context and HTTP status
- Could add `Body()` method to store raw response for debugging

### 5. Configuration

**Official Client**:
```go
type Configuration struct {
    Host             string
    Scheme           string
    DefaultHeader    map[string]string
    UserAgent        string
    Debug            bool
    Servers          ServerConfigurations
    OperationServers map[string]ServerConfigurations
    HTTPClient       *http.Client
}
```

**Our Implementation**:
```go
type Client struct {
    baseURL           string
    httpClient        *http.Client
    timeout           time.Duration
    defaultLimit      int
    minScore          float64
    enabled           bool
    // ... health check fields
}

// Functional options
func WithBaseURL(url string) ClientOption
func WithTimeout(timeout time.Duration) ClientOption
```

**Recommendation**:
- ✅ Our functional options pattern is more idiomatic and cleaner
- Official client's Configuration struct is over-engineered

### 6. Nullable Types

**Official Client**:
```go
type NullableString struct {
    value *string
    isSet bool
}

func (v NullableString) Get() *string
func (v *NullableString) Set(val *string)
func (v NullableString) IsSet() bool
```

**Our Implementation**:
- Uses plain Go types with pointers for optional fields

**Recommendation**:
- ✅ Our approach is simpler
- Nullable types add boilerplate without significant benefit

### 7. HTTP Client Management

**Official Client**:
```go
func (c *APIClient) callAPI(request *http.Request) (*http.Response, error) {
    if c.cfg.Debug {
        log.Printf("Calling API: %s %s", request.Method, request.URL)
    }
    return c.cfg.HTTPClient.Do(request)
}
```

**Our Implementation**:
```go
resp, err := c.httpClient.Do(req)
```

**Recommendation**:
- 🔄 **BORROW**: Add debug logging capability
- Add `WithDebug(bool)` option to log requests/responses

### 8. Request Preparation

**Official Client**:
```go
func (c *APIClient) prepareRequest(
    ctx context.Context,
    path string, method string,
    postBody interface{},
    headerParams map[string]string,
    queryParams url.Values,
    formParams url.Values,
    formFiles []formFile,
) (*http.Request, error)
```

**Our Implementation**:
- Direct request building in each method

**Recommendation**:
- ✅ Our approach is more straightforward
- Official client's abstraction adds complexity without clear benefit

### 9. Response Handling

**Official Client**:
```go
localVarBody, err := io.ReadAll(localVarHTTPResponse.Body)
localVarHTTPResponse.Body.Close()
localVarHTTPResponse.Body = io.NopCloser(bytes.NewBuffer(localVarBody))

if localVarHTTPResponse.StatusCode >= 300 {
    newErr := &GenericOpenAPIError{
        body:  localVarBody,
        error: localVarHTTPResponse.Status,
    }
    // Try to decode error response
    var v HTTPValidationError
    err = a.client.decode(&v, localVarBody, localVarHTTPResponse.StatusCode)
    // ...
}
```

**Our Implementation**:
```go
if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
    return nil, &ClientError{
        Op:      "retain",
        Message: fmt.Sprintf("unexpected status code: %d", resp.StatusCode),
        Status:  resp.StatusCode,
    }
}
```

**Recommendation**:
- 🔄 **BORROW**: Read response body for better error messages
- Store response body in error for debugging

### 10. JSON Handling

**Official Client**:
- Uses `json.Marshal`/`json.Unmarshal` directly
- Custom `ToMap()` method for each type
- Validation in `UnmarshalJSON`

**Our Implementation**:
```go
jsonBody, err := json.Marshal(reqBody)
// ...
err := json.NewDecoder(resp.Body).Decode(&result)
```

**Recommendation**:
- ✅ Our approach is standard and sufficient
- Validation is nice but not critical for our use case

## Key Improvements to Borrow

### Immediate (Phase 2)
1. **Context Support**: Add `context.Context` parameter to all public methods
2. **Debug Logging**: Add `WithDebug(bool)` option
3. **Response Body in Errors**: Store response body in ClientError for debugging

### Future (Phase 3)
4. **Request/Response Logging**: Log all API calls when debug enabled
5. **Metrics**: Track request latency, success/failure rates
6. **Retry Logic**: Add exponential backoff with context cancellation support

## What NOT to Borrow

1. ❌ **Fluent API** - Adds complexity without benefit
2. ❌ **Nullable Types** - Too verbose for our needs
3. ❌ **Configuration Struct** - Functional options are cleaner
4. ❌ **Strongly-typed Request Models** - Maps are more flexible

## Migration Path to Official Client

If we want to migrate to the official client in the future:

1. **Pros**:
   - Officially maintained by Vectorize
   - Auto-generated from OpenAPI spec (always up-to-date)
   - Type safety
   - Community support

2. **Cons**:
   - Heavyweight (many generated files)
   - Fluent API is less idiomatic
   - Nullable types add boilerplate
   - Less control over request formatting

3. **Recommendation**: 
   - Keep our custom client for now
   - Our implementation is simpler and more maintainable
   - Revisit if we need features not in our client

## Action Items

- [ ] Add context support to Retain(), Search(), Reflect() methods
- [ ] Add debug logging option
- [ ] Improve error messages with response body
- [ ] Test async retention with current implementation
- [ ] Monitor batch retention service performance
