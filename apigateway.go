package sparta

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	gocf "github.com/crewjam/go-cloudformation"

	"github.com/Sirupsen/logrus"
)

/*
"context" : {
  "apiId" : "$util.escapeJavaScript($context.apiId)",
  "method" : "$util.escapeJavaScript($context.httpMethod)",
  "requestId" : "$util.escapeJavaScript($context.requestId)",
  "resourceId" : "$util.escapeJavaScript($context.resourceId)",
  "resourcePath" : "$util.escapeJavaScript($context.resourcePath)",
  "stage" : "$util.escapeJavaScript($context.stage)",
  "identity" : {
    "accountId" : "$util.escapeJavaScript($context.identity.accountId)",
    "apiKey" : "$util.escapeJavaScript($context.identity.apiKey)",
    "caller" : "$util.escapeJavaScript($context.identity.caller)",
    "cognitoAuthenticationProvider" : "$util.escapeJavaScript($context.identity.cognitoAuthenticationProvider)",
    "cognitoAuthenticationType" : "$util.escapeJavaScript($context.identity.cognitoAuthenticationType)",
    "cognitoIdentityId" : "$util.escapeJavaScript($context.identity.cognitoIdentityId)",
    "cognitoIdentityPoolId" : "$util.escapeJavaScript($context.identity.cognitoIdentityPoolId)",
    "sourceIp" : "$util.escapeJavaScript($context.identity.sourceIp)",
    "user" : "$util.escapeJavaScript($context.identity.user)",
    "userAgent" : "$util.escapeJavaScript($context.identity.userAgent)",
    "userArn" : "$util.escapeJavaScript($context.identity.userArn)"
  }
*/

const (
	// OutputAPIGatewayURL is the keyname used in the CloudFormation Output
	// that stores the APIGateway provisioned URL
	// @enum OutputKey
	OutputAPIGatewayURL = "APIGatewayURL"
)

// APIGatewayIdentity represents the user identity of a request
// made on behalf of the API Gateway
type APIGatewayIdentity struct {
	// Account ID
	AccountID string `json:"accountId"`
	// API Key
	APIKey string `json:"apiKey"`
	// Caller
	Caller string `json:"caller"`
	// Cognito Authentication Provider
	CognitoAuthenticationProvider string `json:"cognitoAuthenticationProvider"`
	// Cognito Authentication Type
	CognitoAuthenticationType string `json:"cognitoAuthenticationType"`
	// CognitoIdentityId
	CognitoIdentityID string `json:"cognitoIdentityId"`
	// CognitoIdentityPoolId
	CognitoIdentityPoolID string `json:"cognitoIdentityPoolId"`
	// Source IP
	SourceIP string `json:"sourceIp"`
	// User
	User string `json:"user"`
	// User Agent
	UserAgent string `json:"userAgent"`
	// User ARN
	UserARN string `json:"userArn"`
}

// APIGatewayContext represents the context available to an AWS Lambda
// function that is invoked by an API Gateway integration.
type APIGatewayContext struct {
	// API ID
	APIID string `json:"apiId"`
	// HTTPMethod
	Method string `json:"method"`
	// Request ID
	RequestID string `json:"requestId"`
	// Resource ID
	ResourceID string `json:"resourceId"`
	// Resource Path
	ResourcePath string `json:"resourcePath"`
	// Stage
	Stage string `json:"stage"`
	// User identity
	Identity APIGatewayIdentity `json:"identity"`
}

// APIGatewayLambdaJSONEvent provides a pass through mapping
// of all whitelisted Parameters.  The transformation is defined
// by the resources/gateway/inputmapping_json.vtl template.
type APIGatewayLambdaJSONEvent struct {
	// HTTPMethod
	Method string `json:"method"`
	// Body, if available
	Body json.RawMessage `json:"body"`
	// Whitelisted HTTP headers
	Headers map[string]string `json:"headers"`
	// Whitelisted HTTP query params
	QueryParams map[string]string `json:"queryParams"`
	// Whitelisted path parameters
	PathParams map[string]string `json:"pathParams"`
	// Context information - http://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-mapping-template-reference.html#context-variable-reference
	Context APIGatewayContext `json:"context"`
}

// Model proxies the AWS SDK's Model data.  See
// http://docs.aws.amazon.com/sdk-for-go/api/service/apigateway.html#type-Model
//
// TODO: Support Dynamic Model creation
type Model struct {
	Description string `json:",omitempty"`
	Name        string `json:",omitempty"`
	Schema      string `json:",omitempty"`
}

// Response proxies the AWS SDK's PutMethodResponseInput data.  See
// http://docs.aws.amazon.com/sdk-for-go/api/service/apigateway.html#type-PutMethodResponseInput
type Response struct {
	Parameters map[string]bool   `json:",omitempty"`
	Models     map[string]*Model `json:",omitempty"`
}

// IntegrationResponse proxies the AWS SDK's IntegrationResponse data.  See
// http://docs.aws.amazon.com/sdk-for-go/api/service/apigateway.html#type-IntegrationResponse
type IntegrationResponse struct {
	Parameters       map[string]string `json:",omitempty"`
	SelectionPattern string            `json:",omitempty"`
	Templates        map[string]string `json:",omitempty"`
}

// DefaultIntegrationResponses returns a map of HTTP status codes to
// integration response RegExps to return customized HTTP status
// codes to API Gateway clients.  The regexp is triggered by the
// presence of a golang HTTP status string in the error object.
// https://golang.org/src/net/http/status.go
// http://www.jayway.com/2015/11/07/error-handling-in-api-gateway-and-aws-lambda/
func DefaultIntegrationResponses(defaultHTTPStatusCode int) map[int]*IntegrationResponse {
	responseMap := make(map[int]*IntegrationResponse)

	for i := 200; i <= 599; i++ {
		statusText := http.StatusText(i)
		if "" != statusText {
			regExp := fmt.Sprintf(".*%s.*", statusText)
			if i == defaultHTTPStatusCode {
				regExp = ""
			}
			responseMap[i] = &IntegrationResponse{
				SelectionPattern: regExp,
				Templates: map[string]string{
					"application/json": "",
					"text/plain":       "",
				},
			}
		}
	}
	return responseMap
}

// Integration proxies the AWS SDK's Integration data.  See
// http://docs.aws.amazon.com/sdk-for-go/api/service/apigateway.html#type-Integration
type Integration struct {
	Parameters         map[string]string
	RequestTemplates   map[string]string
	CacheKeyParameters []string
	CacheNamespace     string
	Credentials        string

	Responses map[int]*IntegrationResponse

	// Typically "AWS", but for OPTIONS CORS support is set to "MOCK"
	integrationType string
}

// MarshalJSON customizes the JSON representation used when serializing to the
// CloudFormation template representation.
func (integration Integration) MarshalJSON() ([]byte, error) {
	var responses = integration.Responses
	var requestTemplates = integration.RequestTemplates
	// Default RequestTemplates will be inserted by the apigateway.js CustomResource
	// at instantiation time.
	for eachStatusCode := range responses {
		httpString := http.StatusText(eachStatusCode)
		if "" == httpString {
			return nil, fmt.Errorf("Invalid HTTP status code in Integration Response: %d", eachStatusCode)
		}
	}

	var stringResponses = make(map[string]*IntegrationResponse, 0)
	for eachKey, eachValue := range responses {
		stringResponses[strconv.Itoa(eachKey)] = eachValue
	}
	integrationJSON := map[string]interface{}{
		"Responses":        stringResponses,
		"RequestTemplates": requestTemplates,
		"Type":             integration.integrationType,
	}
	if len(integration.Parameters) > 0 {
		integrationJSON["Parameters"] = integration.Parameters
	}
	if len(integration.CacheNamespace) > 0 {
		integrationJSON["CacheNamespace"] = integration.CacheNamespace
	}
	if len(integration.Credentials) > 0 {
		integrationJSON["Credentials"] = integration.Credentials
	}
	if len(integration.CacheKeyParameters) > 0 {
		integrationJSON["CacheKeyParameters"] = integration.CacheKeyParameters
	}
	return json.Marshal(integrationJSON)
}

// Method proxies the AWS SDK's Method data.  See
// http://docs.aws.amazon.com/sdk-for-go/api/service/apigateway.html#type-Method
type Method struct {
	authorizationType string
	httpMethod        string
	APIKeyRequired    bool

	// Request data
	Parameters map[string]bool
	Models     map[string]*Model

	// Response map
	Responses map[int]*Response

	// Integration response map
	Integration Integration
}

// DefaultMethodResponses returns the default set of Method HTTPStatus->Response
// pass through responses.  The successfulHTTPStatusCode param is the single
// 2XX response code to use for the method.
func DefaultMethodResponses(successfulHTTPStatusCode int) map[int]*Response {
	responses := make(map[int]*Response, 0)

	// Only one 2xx status code response may exist on a Method
	responses[successfulHTTPStatusCode] = defaultResponse()

	// Add mappings for the other return codes
	for i := 300; i <= 599; i++ {
		statusText := http.StatusText(i)
		if "" != statusText {
			responses[i] = defaultResponse()
		}
	}
	return responses
}

// Return the default response for the standard response types.
func defaultResponse() *Response {
	contentTypes := []string{"application/json", "text/plain"}
	models := make(map[string]*Model, 0)
	for _, eachContentType := range contentTypes {
		description := "Empty model"
		if eachContentType == "application/json" {
			description = "Empty JSON model"
		} else {
			parts := strings.Split(eachContentType, "/")
			if len(parts) == 2 {
				description = fmt.Sprintf("Empty %s model", strings.ToUpper(parts[0]))
			}
		}
		models[eachContentType] = &Model{
			Description: description,
			Name:        "Empty",
			Schema:      "",
		}
	}
	return &Response{
		Models: models,
	}
}

// MarshalJSON customizes the JSON representation used when serializing to the
// CloudFormation template representation.  If method.Responses is empty, the
// DefaultMethodResponses map will be used, where the HTTP Success code is 201 for POST
// methods and 200 for all other methodnames.
func (method *Method) MarshalJSON() ([]byte, error) {
	responses := method.Responses

	for eachStatusCode := range responses {
		httpString := http.StatusText(eachStatusCode)
		if "" == httpString {
			return nil, fmt.Errorf("Invalid HTTP status code in Method Response: %d", eachStatusCode)
		}
	}

	var stringResponses = make(map[string]*Response, 0)
	for eachKey, eachValue := range responses {
		stringResponses[strconv.Itoa(eachKey)] = eachValue
	}

	return json.Marshal(map[string]interface{}{
		"AuthorizationType": method.authorizationType,
		"HTTPMethod":        method.httpMethod,
		"APIKeyRequired":    method.APIKeyRequired,
		"Parameters":        method.Parameters,
		"Models":            method.Models,
		"Responses":         stringResponses,
		"Integration":       method.Integration,
	})
}

// Resource proxies the AWS SDK's Resource data.  See
// http://docs.aws.amazon.com/sdk-for-go/api/service/apigateway.html#type-Resource
type Resource struct {
	pathPart     string
	parentLambda *LambdaAWSInfo
	Methods      map[string]*Method
}

// MarshalJSON customizes the JSON representation used when serializing to the
// CloudFormation template representation.
func (resource *Resource) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"PathPart":  resource.pathPart,
		"LambdaArn": gocf.GetAtt(resource.parentLambda.logicalName(), "Arn"),
		"Methods":   resource.Methods,
	})
}

// Stage proxies the AWS SDK's Stage data.  See
// http://docs.aws.amazon.com/sdk-for-go/api/service/apigateway.html#type-Stage
type Stage struct {
	name                string
	CacheClusterEnabled bool
	CacheClusterSize    string
	Description         string
	Variables           map[string]string
}

// MarshalJSON customizes the JSON representation used when serializing to the
// CloudFormation template representation.
func (stage *Stage) MarshalJSON() ([]byte, error) {
	stageJSON := map[string]interface{}{
		"Name":                stage.name,
		"CacheClusterEnabled": stage.CacheClusterEnabled,
	}
	if len(stage.CacheClusterSize) > 0 {
		stageJSON["CacheClusterSize"] = stage.CacheClusterSize
	}
	if len(stage.Description) > 0 {
		stageJSON["Description"] = stage.Description
	}
	if len(stage.Variables) > 0 {
		stageJSON["Variables"] = stage.Variables
	}
	return json.Marshal(stageJSON)
}

// API represents the AWS API Gateway data associated with a given Sparta app.  Proxies
// the AWS SDK's CreateRestApiInput data.  See
// http://docs.aws.amazon.com/sdk-for-go/api/service/apigateway.html#type-CreateRestApiInput
type API struct {
	// The API name
	// TOOD: bind this to the stack name to prevent provisioning collisions.
	name string
	// Optional stage. If defined, the API will be deployed
	stage *Stage
	// Existing API to CloneFrom
	CloneFrom string
	// API Description
	Description string
	// Non-empty map of urlPaths->Resource definitions
	resources map[string]*Resource
	// Should CORS be enabled for this API?
	CORSEnabled bool
}

type resourceNode struct {
	PathComponent string
	Children      map[string]*resourceNode
	APIResources  map[string]*Resource
}

// MarshalJSON customizes the JSON representation used when serializing to the
// CloudFormation template representation.
func (api *API) MarshalJSON() ([]byte, error) {

	// Transform the map of resources into a set of hierarchical resourceNodes
	rootResource := resourceNode{
		PathComponent: "/",
		Children:      make(map[string]*resourceNode, 0),
		APIResources:  make(map[string]*Resource, 0),
	}
	for eachPath, eachResource := range api.resources {
		ctxNode := &rootResource
		pathParts := strings.Split(eachPath, "/")[1:]
		// Start at the root and descend
		for _, eachPathPart := range pathParts {
			_, exists := ctxNode.Children[eachPathPart]
			if !exists {
				childNode := &resourceNode{
					PathComponent: eachPathPart,
					Children:      make(map[string]*resourceNode, 0),
					APIResources:  make(map[string]*Resource, 0),
				}
				ctxNode.Children[eachPathPart] = childNode
			}
			ctxNode = ctxNode.Children[eachPathPart]
		}
		ctxNode.APIResources[eachResource.parentLambda.logicalName()] = eachResource
	}

	apiJSON := map[string]interface{}{
		"Name":      api.name,
		"Resources": rootResource,
	}
	if len(api.CloneFrom) > 0 {
		apiJSON["CloneFrom"] = api.CloneFrom
	}
	if len(api.Description) > 0 {
		apiJSON["Description"] = api.Description
	}
	if nil != api.stage {
		apiJSON["Stage"] = api.stage
	}
	return json.Marshal(apiJSON)
}

// export marshals the API data to a CloudFormation compatible representation
func (api *API) export(S3Bucket string,
	S3Key string,
	roleNameMap map[string]*gocf.StringExpr,
	template *gocf.Template,
	logger *logrus.Logger) error {

	lambdaResourceName, err := ensureConfiguratorLambdaResource(APIGatewayPrincipal,
		gocf.String("*"),
		[]string{},
		template,
		S3Bucket,
		S3Key,
		logger)

	if nil != err {
		return err
	}

	// If this API is CORS enabled, then annotate the APIResources with OPTION
	// entries.  Slight overhead in network I/O due to marshalling data, but simplifies
	// the CustomResource, which is only a temporary stopgap until cloudformation
	// properly supports APIGateway
	responseParameters := map[string]bool{
		"method.response.header.Access-Control-Allow-Headers": true,
		"method.response.header.Access-Control-Allow-Methods": true,
		"method.response.header.Access-Control-Allow-Origin":  true,
	}
	integrationResponseParameters := map[string]string{
		"method.response.header.Access-Control-Allow-Headers": "'Content-Type,X-Amz-Date,Authorization,X-Api-Key'",
		"method.response.header.Access-Control-Allow-Methods": "'*'",
		"method.response.header.Access-Control-Allow-Origin":  "'*'",
	}
	// Keep track of how many resources && methods we're supposed to provision.  If there
	// aren't any, then throw an error
	resourceCount := 0
	methodCount := 0
	// We need to update the default values here, because the individual
	// methods are deserialized they annotate the prexisting responses with whitelist data.
	for _, eachResource := range api.resources {
		resourceCount++
		if api.CORSEnabled {
			// Create the OPTIONS entry
			method, methodErr := eachResource.NewMethod("OPTIONS")
			if methodErr != nil {
				return methodErr
			}
			methodCount++

			statusOkResponse := defaultResponse()
			statusOkResponse.Parameters = responseParameters
			method.Responses[200] = statusOkResponse

			method.Integration = Integration{
				Parameters:       make(map[string]string, 0),
				RequestTemplates: make(map[string]string, 0),
				Responses:        make(map[int]*IntegrationResponse, 0),
				integrationType:  "MOCK",
			}
			method.Integration.RequestTemplates["application/json"] = "{\"statusCode\": 200}"
			corsIntegrationResponse := IntegrationResponse{
				Parameters: integrationResponseParameters,
				Templates: map[string]string{
					"application/json": "",
				},
			}
			method.Integration.Responses[200] = &corsIntegrationResponse
		}

		for _, eachMethod := range eachResource.Methods {
			methodCount++
			statusSuccessfulCode := http.StatusOK
			if eachMethod.httpMethod == "POST" {
				statusSuccessfulCode = http.StatusCreated
			}

			if len(eachMethod.Responses) <= 0 {
				eachMethod.Responses = DefaultMethodResponses(statusSuccessfulCode)
			}
			if api.CORSEnabled {
				for _, eachResponse := range eachMethod.Responses {
					if nil == eachResponse.Parameters {
						eachResponse.Parameters = make(map[string]bool, 0)
					}
					for eachKey, eachBool := range responseParameters {
						eachResponse.Parameters[eachKey] = eachBool
					}
				}
			}
			// Update Integration
			if len(eachMethod.Integration.Responses) <= 0 {
				eachMethod.Integration.Responses = DefaultIntegrationResponses(statusSuccessfulCode)
			}
			if api.CORSEnabled {
				for eachHTTPStatus, eachIntegrationResponse := range eachMethod.Integration.Responses {
					if eachHTTPStatus >= 200 && eachHTTPStatus <= 299 {
						if nil == eachIntegrationResponse.Parameters {
							eachIntegrationResponse.Parameters = make(map[string]string, 0)
						}
						for eachKey, eachValue := range integrationResponseParameters {
							eachIntegrationResponse.Parameters[eachKey] = eachValue
						}
					}
				}
			}
		}
	}

	if resourceCount <= 0 || methodCount <= 0 {
		logger.WithFields(logrus.Fields{
			"ResourceCount": resourceCount,
			"MethodCount":   methodCount,
		}).Error("*sparta.API value provided to sparta.Main(), but no resources or methods were defined")
		return errors.New("Non-nil, empty *sparta.API provided to sparta.Main(). Prefer `nil` value")
	}

	// Unmarshal everything to JSON
	newResource, err := newCloudFormationResource("Custom::SpartaAPIGateway", logger)
	if nil != err {
		return err
	}
	apiGatewayResource := newResource.(*cloudFormationAPIGatewayResource)
	apiGatewayResource.ServiceToken = gocf.GetAtt(lambdaResourceName, "Arn")
	apiGatewayResource.API = api

	apiGatewayInvokerResName := CloudFormationResourceName("APIGateway", api.name)
	cfResource := template.AddResource(apiGatewayInvokerResName, apiGatewayResource)
	cfResource.DependsOn = append(cfResource.DependsOn, lambdaResourceName)

	// Save the output
	template.Outputs[OutputAPIGatewayURL] = &gocf.Output{
		Description: "API Gateway URL",
		Value:       gocf.GetAtt(apiGatewayInvokerResName, "URL"),
	}
	return nil
}

func (api *API) logicalName() string {
	return CloudFormationResourceName("APIGateway", api.name, api.stage.name)
}

// NewAPIGateway returns a new API Gateway structure.  If stage is defined, the API Gateway
// will also be deployed as part of stack creation.
func NewAPIGateway(name string, stage *Stage) *API {
	return &API{
		name:      name,
		stage:     stage,
		resources: make(map[string]*Resource, 0),
	}
}

// NewStage returns a Stage object with the given name.  Providing a Stage value
// to NewAPIGateway implies that the API Gateway resources should be deployed
// (eg: made publicly accessible).  See
// http://docs.aws.amazon.com/apigateway/latest/developerguide/how-to-deploy-api.html
func NewStage(name string) *Stage {
	return &Stage{
		name:      name,
		Variables: make(map[string]string, 0),
	}
}

// NewResource associates a URL path value with the LambdaAWSInfo golang lambda.  To make
// the Resource available, associate one or more Methods via NewMethod().
func (api *API) NewResource(pathPart string, parentLambda *LambdaAWSInfo) (*Resource, error) {
	_, exists := api.resources[pathPart]
	if exists {
		return nil, fmt.Errorf("Path %s already defined for lambda function: %s", pathPart, parentLambda.lambdaFnName)
	}
	resource := &Resource{
		pathPart:     pathPart,
		parentLambda: parentLambda,
		Methods:      make(map[string]*Method, 0),
	}
	api.resources[pathPart] = resource
	return resource, nil
}

// NewMethod associates the httpMethod name with the given Resource.  The returned Method
// has no authorization requirements.
func (resource *Resource) NewMethod(httpMethod string) (*Method, error) {
	authorizationType := "NONE"

	// http://docs.aws.amazon.com/apigateway/latest/developerguide/how-to-method-settings.html#how-to-method-settings-console
	keyname := httpMethod
	_, exists := resource.Methods[keyname]
	if exists {
		errMsg := fmt.Sprintf("Method %s (Auth: %s) already defined for resource", httpMethod, authorizationType)
		return nil, errors.New(errMsg)
	}
	integration := Integration{
		Parameters:       make(map[string]string, 0),
		RequestTemplates: make(map[string]string, 0),
		Responses:        make(map[int]*IntegrationResponse, 0),
		integrationType:  "AWS", // Type used for Lambda integration
	}

	method := &Method{
		authorizationType: authorizationType,
		httpMethod:        httpMethod,
		Parameters:        make(map[string]bool, 0),
		Models:            make(map[string]*Model, 0),
		Responses:         make(map[int]*Response, 0),
		Integration:       integration,
	}
	resource.Methods[keyname] = method
	return method, nil
}

// NewAuthorizedMethod associates the httpMethod name and authorizationType with the given Resource.
func (resource *Resource) NewAuthorizedMethod(httpMethod string, authorizationType string) (*Method, error) {
	method, err := resource.NewMethod(httpMethod)
	if nil != err {
		method.authorizationType = authorizationType
	}
	return method, err
}
