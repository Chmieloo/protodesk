package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"protodesk/pkg/models"
	"protodesk/pkg/models/proto"
	"protodesk/pkg/services"

	"github.com/google/uuid"
	"github.com/jhump/protoreflect/dynamic"
	"github.com/jhump/protoreflect/grpcreflect"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"google.golang.org/grpc/metadata"
	reflectpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
)

// App struct represents the main application
type App struct {
	ctx            context.Context
	profileManager *services.ServerProfileManager
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// Startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) Startup(ctx context.Context) error {
	fmt.Println("[Startup] Startup called")
	a.ctx = ctx

	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("[Startup] Failed to get user home directory:", err)
		return fmt.Errorf("failed to get user home directory: %w", err)
	}
	fmt.Println("[Startup] Home directory:", homeDir)

	dataDir := filepath.Join(homeDir, ".protodesk")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		fmt.Println("[Startup] Failed to create data directory:", err)
		return fmt.Errorf("failed to create data directory: %w", err)
	}
	fmt.Println("[Startup] Data directory:", dataDir)

	store, err := services.NewSQLiteStore(dataDir)
	if err != nil {
		fmt.Println("[Startup] Failed to initialize server profile store:", err)
		return fmt.Errorf("failed to initialize server profile store: %w", err)
	}
	fmt.Println("[Startup] Server profile store initialized")

	a.profileManager = services.NewServerProfileManager(store)
	fmt.Println("[Startup] profileManager initialized successfully")
	return nil
}

// CreateServerProfile creates a new server profile
func (a *App) CreateServerProfile(name string, host string, port int, enableTLS bool, certPath *string, useReflection bool, headers []models.Header) (*models.ServerProfile, error) {
	if a.profileManager == nil {
		return nil, fmt.Errorf("profileManager is not initialized (did Startup run successfully?)")
	}
	profile := models.NewServerProfile(name, host, port)
	profile.TLSEnabled = enableTLS
	profile.CertificatePath = certPath
	profile.UseReflection = useReflection
	profile.Headers = headers

	if err := profile.Validate(); err != nil {
		return nil, err
	}

	if err := a.profileManager.GetStore().Create(a.ctx, profile); err != nil {
		return nil, fmt.Errorf("failed to create server profile: %w", err)
	}

	return profile, nil
}

// GetServerProfile retrieves a server profile by ID
func (a *App) GetServerProfile(id string) (*models.ServerProfile, error) {
	return a.profileManager.GetStore().Get(a.ctx, id)
}

// ListServerProfiles returns all server profiles
func (a *App) ListServerProfiles() ([]*models.ServerProfile, error) {
	return a.profileManager.GetStore().List(a.ctx)
}

// UpdateServerProfile updates an existing server profile
func (a *App) UpdateServerProfile(profile *models.ServerProfile) error {
	if err := profile.Validate(); err != nil {
		return err
	}
	return a.profileManager.GetStore().Update(a.ctx, profile)
}

// DeleteServerProfile deletes a server profile by ID
func (a *App) DeleteServerProfile(id string) error {
	// Disconnect if connected
	if a.profileManager.IsConnected(id) {
		if err := a.profileManager.Disconnect(a.ctx, id); err != nil {
			return fmt.Errorf("failed to disconnect before deletion: %w", err)
		}
	}
	return a.profileManager.GetStore().Delete(a.ctx, id)
}

// ConnectToServer establishes a connection to a server profile
func (a *App) ConnectToServer(id string) error {
	return a.profileManager.Connect(a.ctx, id)
}

// DisconnectFromServer closes the connection to a server profile
func (a *App) DisconnectFromServer(id string) error {
	return a.profileManager.Disconnect(a.ctx, id)
}

// IsServerConnected checks if a server profile is currently connected
func (a *App) IsServerConnected(id string) bool {
	return a.profileManager.IsConnected(id)
}

// Shutdown handles cleanup when the application exits
func (a *App) Shutdown(ctx context.Context) {
	a.profileManager.DisconnectAll()
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

// SaveProtoDefinition saves a parsed proto definition to storage
func (a *App) SaveProtoDefinition(def *proto.ProtoDefinition) error {
	return a.profileManager.GetStore().CreateProtoDefinition(a.ctx, def)
}

// ListProtoDefinitionsByProfile lists proto definitions for a server profile
func (a *App) ListProtoDefinitionsByProfile(profileID string) ([]*proto.ProtoDefinition, error) {
	return a.profileManager.GetStore().ListProtoDefinitionsByProfile(a.ctx, profileID)
}

// DeleteProtoDefinition deletes a proto definition by ID
func (a *App) DeleteProtoDefinition(id string) error {
	return a.profileManager.GetStore().DeleteProtoDefinition(a.ctx, id)
}

// ProtoFileImport represents a proto file found in a folder
// to be returned to the frontend
type ProtoFileImport struct {
	FilePath string `json:"filePath"`
	Content  string `json:"content"`
}

// ImportProtoFilesFromFolder opens a folder picker, recursively finds all .proto files, and returns their paths and contents
func (a *App) ImportProtoFilesFromFolder() ([]ProtoFileImport, error) {
	if a.ctx == nil {
		return nil, fmt.Errorf("context not initialized")
	}
	// Open folder picker dialog
	folder, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select a folder containing proto files",
	})
	if err != nil {
		return nil, err
	}
	if folder == "" {
		return nil, nil // user cancelled
	}
	var results []ProtoFileImport
	err = filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) == ".proto" {
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			results = append(results, ProtoFileImport{
				FilePath: path,
				Content:  string(content),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// SelectProtoFolder opens a directory dialog and returns the selected folder path
func (a *App) SelectProtoFolder() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("context not initialized")
	}
	folder, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select a proto folder",
	})
	if err != nil {
		return "", err
	}
	return folder, nil
}

// ScanAndParseProtoPath scans a proto path, parses all .proto files, and stores results in the DB
func (a *App) ScanAndParseProtoPath(serverID, protoPathID, path string) ([]*proto.ProtoDefinition, error) {
	parser := proto.NewParser([]string{path})
	var results []*proto.ProtoDefinition
	err := filepath.Walk(path, func(file string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(file) != ".proto" {
			return nil
		}
		content, readErr := os.ReadFile(file)
		if readErr != nil {
			return readErr
		}
		pd := proto.NewProtoDefinition(file, string(content))
		pd.ServerProfileID = serverID
		pd.ProtoPathID = protoPathID
		pd.LastParsed = time.Now()
		// Try to parse
		parsed, parseErr := parser.ParseFile(file)
		if parseErr != nil {
			pd.Error = parseErr.Error()
		} else {
			pd.Imports = parsed.Imports
			pd.Services = parsed.Services
			pd.Error = ""
		}
		// Save or update in DB
		existing, _ := a.profileManager.GetStore().GetProtoDefinition(a.ctx, pd.ID)
		if existing != nil {
			_ = a.profileManager.GetStore().UpdateProtoDefinition(a.ctx, pd)
		} else {
			pd.ID = uuid.New().String()
			_ = a.profileManager.GetStore().CreateProtoDefinition(a.ctx, pd)
		}
		results = append(results, pd)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// CreateProtoPath creates a proto path record in the database and links it to a server profile
func (a *App) CreateProtoPath(id, serverProfileId, path string) error {
	if a.profileManager == nil {
		return fmt.Errorf("profile manager not initialized; startup may not have run successfully")
	}
	protoPath := &services.ProtoPath{
		ID:              id,
		ServerProfileID: serverProfileId,
		Path:            path,
	}
	return a.profileManager.GetStore().CreateProtoPath(context.Background(), protoPath)
}

// ListProtoPathsByServer lists proto paths for a given server profile
func (a *App) ListProtoPathsByServer(serverID string) ([]*services.ProtoPath, error) {
	if a.profileManager == nil {
		return nil, fmt.Errorf("profile manager not initialized; startup may not have run successfully")
	}
	return a.profileManager.GetStore().ListProtoPathsByServer(context.Background(), serverID)
}

// DeleteProtoPath deletes a proto path by its ID
func (a *App) DeleteProtoPath(id string) error {
	if a.profileManager == nil {
		return fmt.Errorf("profile manager not initialized; startup may not have run successfully")
	}
	return a.profileManager.GetStore().DeleteProtoPath(context.Background(), id)
}

// ConnectServer establishes a connection to the specified server profile
func (a *App) ConnectServer(ctx context.Context, profileID string) error {
	return a.profileManager.Connect(ctx, profileID)
}

// ListServerServices returns all services and their methods for a connected server using reflection
func (a *App) ListServerServices(profileID string) (map[string][]string, error) {
	if a.profileManager == nil {
		return nil, fmt.Errorf("profileManager is not initialized")
	}
	conn, err := a.profileManager.GetConnection(profileID)
	if err != nil {
		return nil, fmt.Errorf("no active connection for profile %s: %w", profileID, err)
	}
	return a.profileManager.GetGRPCClient().ListServicesAndMethods(conn)
}

// GetMethodInputDescriptor returns the input fields for a given service/method using reflection
func (a *App) GetMethodInputDescriptor(profileID, serviceName, methodName string) ([]services.FieldDescriptor, error) {
	if a.profileManager == nil {
		return nil, fmt.Errorf("profileManager is not initialized")
	}
	conn, err := a.profileManager.GetConnection(profileID)
	if err != nil {
		return nil, fmt.Errorf("no active connection for profile %s: %w", profileID, err)
	}
	return a.profileManager.GetGRPCClient().GetMethodInputDescriptor(conn, serviceName, methodName)
}

// SavePerRequestHeaders saves or updates per-request headers for a method
func (a *App) SavePerRequestHeaders(serverProfileID, serviceName, methodName, headersJSON string) error {
	h := &models.PerRequestHeaders{
		ServerProfileID: serverProfileID,
		ServiceName:     serviceName,
		MethodName:      methodName,
		HeadersJSON:     headersJSON,
	}
	return a.profileManager.GetStore().UpsertPerRequestHeaders(a.ctx, h)
}

// GetPerRequestHeaders retrieves per-request headers for a method
func (a *App) GetPerRequestHeaders(serverProfileID, serviceName, methodName string) (string, error) {
	h, err := a.profileManager.GetStore().GetPerRequestHeaders(a.ctx, serverProfileID, serviceName, methodName)
	if err != nil {
		return "", err
	}
	return h.HeadersJSON, nil
}

// CallGRPCMethod calls a gRPC method and returns the response as JSON
func (a *App) CallGRPCMethod(
	profileID string,
	serviceName string,
	methodName string,
	requestJSON string,
	headersJSON string,
) (string, error) {
	// 1. Get connection
	conn, err := a.profileManager.GetConnection(profileID)
	if err != nil {
		return "", fmt.Errorf("no active connection for profile %s: %w", profileID, err)
	}

	// 2. Set up reflection client
	ctx := context.Background()
	rc := grpcreflect.NewClient(ctx, reflectpb.NewServerReflectionClient(conn))
	defer rc.Reset()

	svcDesc, err := rc.ResolveService(serviceName)
	if err != nil {
		return "", fmt.Errorf("service not found: %w", err)
	}
	mDesc := svcDesc.FindMethodByName(methodName)
	if mDesc == nil {
		return "", fmt.Errorf("method not found: %s", methodName)
	}

	// 3. Build request message dynamically
	inputType := mDesc.GetInputType()
	reqMsg := dynamic.NewMessage(inputType)
	if err := reqMsg.UnmarshalJSON([]byte(requestJSON)); err != nil {
		return "", fmt.Errorf("failed to unmarshal request: %w", err)
	}

	// 4. Set up headers
	md := metadata.New(nil)
	if headersJSON != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(headersJSON), &headers); err == nil {
			for k, v := range headers {
				md.Append(k, v)
			}
		}
	}
	ctx = metadata.NewOutgoingContext(ctx, md)

	// 5. Invoke the method
	outType := mDesc.GetOutputType()
	respMsg := dynamic.NewMessage(outType)
	methodFullName := fmt.Sprintf("/%s/%s", serviceName, methodName)
	err = conn.Invoke(ctx, methodFullName, reqMsg, respMsg)
	if err != nil {
		return "", fmt.Errorf("gRPC call failed: %w", err)
	}

	// 6. Marshal response to JSON
	respJSON, err := respMsg.MarshalJSON()
	if err != nil {
		return "", fmt.Errorf("failed to marshal response: %w", err)
	}
	return string(respJSON), nil
}
