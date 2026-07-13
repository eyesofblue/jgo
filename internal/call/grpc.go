package call

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bufbuild/protocompile"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// GRPCConfig configures one descriptor-driven unary gRPC request.
type GRPCConfig struct {
	Root    string
	Method  string
	Address string
	Data    string
	Headers []string
	Timeout time.Duration
}

// GRPCMethod describes one method from protobuf descriptors.
type GRPCMethod struct {
	FullName        string
	ClientStreaming bool
	ServerStreaming bool
}

// GRPCResult contains a formatted protobuf JSON response.
type GRPCResult struct {
	Body []byte
}

// CallGRPC resolves a method using Reflection, falls back to local proto
// descriptors, and performs a dynamic unary invocation.
func CallGRPC(ctx context.Context, config GRPCConfig) (GRPCResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if config.Root == "" {
		config.Root = "."
	}
	if config.Timeout <= 0 {
		config.Timeout = 10 * time.Second
	}
	if strings.TrimSpace(config.Address) == "" {
		return GRPCResult{}, fmt.Errorf("call grpc: --addr is required")
	}
	serviceName, methodName, err := splitGRPCMethod(config.Method)
	if err != nil {
		return GRPCResult{}, err
	}
	callContext, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()
	callContext, err = outgoingMetadata(callContext, config.Headers)
	if err != nil {
		return GRPCResult{}, fmt.Errorf("call grpc: %w", err)
	}

	connection, err := grpc.NewClient(strings.TrimSpace(config.Address), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return GRPCResult{}, fmt.Errorf("call grpc: connect: %w", err)
	}
	defer connection.Close()

	descriptor, reflectionAvailable, reflectionErr := resolveReflection(callContext, connection, serviceName, methodName)
	if descriptor == nil {
		localDescriptor, localAvailable, localErr := resolveLocal(callContext, config.Root, serviceName, methodName)
		if localDescriptor == nil {
			available := mergeMethods(reflectionAvailable, localAvailable)
			if localErr != nil {
				return GRPCResult{}, withAvailable(fmt.Errorf("call grpc: reflection failed: %v; local descriptors failed: %w", reflectionErr, localErr), available)
			}
			return GRPCResult{}, withAvailable(fmt.Errorf("call grpc: method %q not found", config.Method), available)
		}
		descriptor = localDescriptor
	}
	if descriptor.IsStreamingClient() || descriptor.IsStreamingServer() {
		return GRPCResult{}, fmt.Errorf("call grpc: streaming method %s is not supported yet", descriptor.FullName())
	}

	request := dynamicpb.NewMessage(descriptor.Input())
	data := strings.TrimSpace(config.Data)
	if data == "" {
		data = "{}"
	}
	if err := (protojson.UnmarshalOptions{}).Unmarshal([]byte(data), request); err != nil {
		return GRPCResult{}, fmt.Errorf("call grpc: decode --data for %s: %w", descriptor.Input().FullName(), err)
	}
	response := dynamicpb.NewMessage(descriptor.Output())
	fullMethod := "/" + string(descriptor.Parent().FullName()) + "/" + string(descriptor.Name())
	if err := connection.Invoke(callContext, fullMethod, request, response); err != nil {
		return GRPCResult{}, fmt.Errorf("call grpc: invoke %s: %w", fullMethod, err)
	}
	encoded, err := (protojson.MarshalOptions{Indent: "  "}).Marshal(response)
	if err != nil {
		return GRPCResult{}, fmt.Errorf("call grpc: encode response: %w", err)
	}
	return GRPCResult{Body: append(encoded, '\n')}, nil
}

// ListGRPC compiles local protobuf sources and lists their services and methods.
func ListGRPC(ctx context.Context, root string) ([]GRPCMethod, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	files, err := compileLocalProtos(ctx, root)
	if err != nil {
		if os.IsNotExist(rootCause(err)) {
			return nil, nil
		}
		return nil, err
	}
	return methodsFromFiles(files), nil
}

func resolveReflection(ctx context.Context, connection grpc.ClientConnInterface, serviceInput, methodInput string) (protoreflect.MethodDescriptor, []string, error) {
	stream, err := reflectionpb.NewServerReflectionClient(connection).ServerReflectionInfo(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer stream.CloseSend()
	if err := stream.Send(&reflectionpb.ServerReflectionRequest{
		MessageRequest: &reflectionpb.ServerReflectionRequest_ListServices{ListServices: ""},
	}); err != nil {
		return nil, nil, err
	}
	response, err := stream.Recv()
	if err != nil {
		return nil, nil, err
	}
	if reflectionError := response.GetErrorResponse(); reflectionError != nil {
		return nil, nil, fmt.Errorf("reflection error %d: %s", reflectionError.ErrorCode, reflectionError.ErrorMessage)
	}
	list := response.GetListServicesResponse()
	if list == nil {
		return nil, nil, fmt.Errorf("reflection returned no service list")
	}
	serviceNames := make([]string, 0, len(list.Service))
	for _, service := range list.Service {
		serviceNames = append(serviceNames, service.Name)
	}
	fullService, err := matchService(serviceNames, serviceInput)
	if err != nil {
		return nil, nil, err
	}
	if err := stream.Send(&reflectionpb.ServerReflectionRequest{
		MessageRequest: &reflectionpb.ServerReflectionRequest_FileContainingSymbol{FileContainingSymbol: fullService},
	}); err != nil {
		return nil, nil, err
	}
	response, err = stream.Recv()
	if err != nil {
		return nil, nil, err
	}
	if reflectionError := response.GetErrorResponse(); reflectionError != nil {
		return nil, nil, fmt.Errorf("reflection error %d: %s", reflectionError.ErrorCode, reflectionError.ErrorMessage)
	}
	fileResponse := response.GetFileDescriptorResponse()
	if fileResponse == nil {
		return nil, nil, fmt.Errorf("reflection returned no descriptor for %s", fullService)
	}
	set := &descriptorpb.FileDescriptorSet{}
	for _, encoded := range fileResponse.FileDescriptorProto {
		file := &descriptorpb.FileDescriptorProto{}
		if err := proto.Unmarshal(encoded, file); err != nil {
			return nil, nil, fmt.Errorf("decode reflected descriptor: %w", err)
		}
		set.File = append(set.File, file)
	}
	files, err := protodesc.NewFiles(set)
	if err != nil {
		return nil, nil, fmt.Errorf("link reflected descriptors: %w", err)
	}
	descriptor, err := files.FindDescriptorByName(protoreflect.FullName(fullService))
	if err != nil {
		return nil, nil, err
	}
	service, ok := descriptor.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, nil, fmt.Errorf("reflected symbol %s is not a service", fullService)
	}
	available := methodsFromService(service)
	method := service.Methods().ByName(protoreflect.Name(methodInput))
	if method == nil {
		return nil, available, fmt.Errorf("method %s.%s not found", fullService, methodInput)
	}
	return method, available, nil
}

func resolveLocal(ctx context.Context, root, serviceInput, methodInput string) (protoreflect.MethodDescriptor, []string, error) {
	files, err := compileLocalProtos(ctx, root)
	if err != nil {
		return nil, nil, err
	}
	serviceNames := localServiceNames(files)
	fullService, err := matchService(serviceNames, serviceInput)
	if err != nil {
		return nil, grpcMethodNames(methodsFromFiles(files)), err
	}
	for _, file := range files {
		services := file.Services()
		for index := 0; index < services.Len(); index++ {
			service := services.Get(index)
			if string(service.FullName()) != fullService {
				continue
			}
			available := methodsFromService(service)
			method := service.Methods().ByName(protoreflect.Name(methodInput))
			if method == nil {
				return nil, available, fmt.Errorf("method %s.%s not found", fullService, methodInput)
			}
			return method, available, nil
		}
	}
	return nil, nil, fmt.Errorf("service %s not found", fullService)
}

func compileLocalProtos(ctx context.Context, root string) ([]protoreflect.FileDescriptor, error) {
	if root == "" {
		root = "."
	}
	directory := filepath.Join(root, "api", "proto")
	var paths []string
	err := filepath.WalkDir(directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 && filepath.Ext(entry.Name()) == ".proto" {
			return fmt.Errorf("call grpc: refusing protobuf symlink %s", path)
		}
		if entry.Type().IsRegular() && filepath.Ext(entry.Name()) == ".proto" {
			relative, err := filepath.Rel(directory, path)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("call grpc: scan local proto: %w", err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("call grpc: no local proto files under %s", directory)
	}
	sort.Strings(paths)
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{ImportPaths: []string{directory}}),
	}
	compiled, err := compiler.Compile(ctx, paths...)
	if err != nil {
		return nil, fmt.Errorf("call grpc: compile local proto: %w", err)
	}
	files := make([]protoreflect.FileDescriptor, len(compiled))
	for index, file := range compiled {
		files[index] = file
	}
	return files, nil
}

func splitGRPCMethod(value string) (string, string, error) {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if strings.Contains(value, "/") {
		service, method, found := strings.Cut(value, "/")
		if found && service != "" && method != "" && !strings.Contains(method, "/") {
			return service, method, nil
		}
	}
	index := strings.LastIndex(value, ".")
	if index <= 0 || index == len(value)-1 {
		return "", "", fmt.Errorf("call grpc: method must be Service.Method or package.Service/Method")
	}
	return value[:index], value[index+1:], nil
}

func matchService(names []string, input string) (string, error) {
	var matches []string
	for _, name := range names {
		if name == input || strings.HasSuffix(name, "."+input) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("service %q not found", input)
	}
	sort.Strings(matches)
	return "", fmt.Errorf("service %q is ambiguous: %s", input, strings.Join(matches, ", "))
}

func outgoingMetadata(ctx context.Context, headers []string) (context.Context, error) {
	parsed, err := parseHeaders(headers)
	if err != nil {
		return nil, err
	}
	pairs := make([]string, 0, len(parsed)*2)
	for name, values := range parsed {
		name = strings.ToLower(name)
		for _, value := range values {
			pairs = append(pairs, name, value)
		}
	}
	return metadata.NewOutgoingContext(ctx, metadata.Pairs(pairs...)), nil
}

func localServiceNames(files []protoreflect.FileDescriptor) []string {
	var names []string
	for _, file := range files {
		services := file.Services()
		for index := 0; index < services.Len(); index++ {
			names = append(names, string(services.Get(index).FullName()))
		}
	}
	return names
}

func methodsFromFiles(files []protoreflect.FileDescriptor) []GRPCMethod {
	var methods []GRPCMethod
	for _, file := range files {
		services := file.Services()
		for index := 0; index < services.Len(); index++ {
			service := services.Get(index)
			for methodIndex := 0; methodIndex < service.Methods().Len(); methodIndex++ {
				method := service.Methods().Get(methodIndex)
				methods = append(methods, GRPCMethod{
					FullName:        string(service.FullName()) + "." + string(method.Name()),
					ClientStreaming: method.IsStreamingClient(), ServerStreaming: method.IsStreamingServer(),
				})
			}
		}
	}
	sort.Slice(methods, func(i, j int) bool { return methods[i].FullName < methods[j].FullName })
	return methods
}

func methodsFromService(service protoreflect.ServiceDescriptor) []string {
	methods := make([]string, 0, service.Methods().Len())
	for index := 0; index < service.Methods().Len(); index++ {
		methods = append(methods, string(service.FullName())+"."+string(service.Methods().Get(index).Name()))
	}
	sort.Strings(methods)
	return methods
}

func grpcMethodNames(methods []GRPCMethod) []string {
	names := make([]string, len(methods))
	for index, method := range methods {
		names[index] = method.FullName
	}
	return names
}

func mergeMethods(groups ...[]string) []string {
	unique := map[string]bool{}
	for _, group := range groups {
		for _, method := range group {
			unique[method] = true
		}
	}
	methods := make([]string, 0, len(unique))
	for method := range unique {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	return methods
}
