// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package connector

import (
	context "context"
	empty "github.com/golang/protobuf/ptypes/empty"
	common "github.com/telepresenceio/telepresence/rpc/v2/common"
	manager "github.com/telepresenceio/telepresence/rpc/v2/manager"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion7

// ConnectorClient is the client API for Connector service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type ConnectorClient interface {
	// Returns version information from the Connector
	Version(ctx context.Context, in *empty.Empty, opts ...grpc.CallOption) (*common.VersionInfo, error)
	// Connects to the cluster and connects the laptop's network (via
	// the daemon process) to the cluster's network.  A result code of
	// UNSPECIFIED indicates that the connection was successfully
	// initiated; if already connected, then either ALREADY_CONNECTED or
	// MUST_RESTART is returned, based on whether the current connection
	// is in agreement with the ConnectionRequest.
	Connect(ctx context.Context, in *ConnectRequest, opts ...grpc.CallOption) (*ConnectInfo, error)
	// Status is much like Connect, except that it doesn't actually do
	// anything.  It's a dry-run.
	Status(ctx context.Context, in *ConnectRequest, opts ...grpc.CallOption) (*ConnectInfo, error)
	// Adds an intercept to a workload.  Requires having already called
	// Connect.
	CreateIntercept(ctx context.Context, in *CreateInterceptRequest, opts ...grpc.CallOption) (*InterceptResult, error)
	// Deactivates and removes an existent workload intercept.
	// Requires having already called Connect.
	RemoveIntercept(ctx context.Context, in *manager.RemoveInterceptRequest2, opts ...grpc.CallOption) (*InterceptResult, error)
	// Uninstalls traffic-agents and traffic-manager from the cluster.
	// Requires having already called Connect.
	Uninstall(ctx context.Context, in *UninstallRequest, opts ...grpc.CallOption) (*UninstallResult, error)
	// Returns a list of workloads and their current intercept status.
	// Requires having already called Connect.
	List(ctx context.Context, in *ListRequest, opts ...grpc.CallOption) (*WorkloadInfoSnapshot, error)
	// Returns a stream of messages to display to the user.  Does NOT
	// require having called anything else first.
	UserNotifications(ctx context.Context, in *empty.Empty, opts ...grpc.CallOption) (Connector_UserNotificationsClient, error)
	Login(ctx context.Context, in *LoginRequest, opts ...grpc.CallOption) (*LoginResult, error)
	// Returns an error with code=NotFound if not currently logged in.
	Logout(ctx context.Context, in *empty.Empty, opts ...grpc.CallOption) (*empty.Empty, error)
	GetCloudUserInfo(ctx context.Context, in *UserInfoRequest, opts ...grpc.CallOption) (*UserInfo, error)
	GetCloudAPIKey(ctx context.Context, in *KeyRequest, opts ...grpc.CallOption) (*KeyData, error)
	GetCloudLicense(ctx context.Context, in *LicenseRequest, opts ...grpc.CallOption) (*LicenseData, error)
	// SetLogLevel will temporarily set the log-level for the daemon for a duration that is determined by the request.
	SetLogLevel(ctx context.Context, in *manager.LogLevelRequest, opts ...grpc.CallOption) (*empty.Empty, error)
	// Quits (terminates) the connector process.
	Quit(ctx context.Context, in *empty.Empty, opts ...grpc.CallOption) (*empty.Empty, error)
}

type connectorClient struct {
	cc grpc.ClientConnInterface
}

func NewConnectorClient(cc grpc.ClientConnInterface) ConnectorClient {
	return &connectorClient{cc}
}

func (c *connectorClient) Version(ctx context.Context, in *empty.Empty, opts ...grpc.CallOption) (*common.VersionInfo, error) {
	out := new(common.VersionInfo)
	err := c.cc.Invoke(ctx, "/telepresence.connector.Connector/Version", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *connectorClient) Connect(ctx context.Context, in *ConnectRequest, opts ...grpc.CallOption) (*ConnectInfo, error) {
	out := new(ConnectInfo)
	err := c.cc.Invoke(ctx, "/telepresence.connector.Connector/Connect", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *connectorClient) Status(ctx context.Context, in *ConnectRequest, opts ...grpc.CallOption) (*ConnectInfo, error) {
	out := new(ConnectInfo)
	err := c.cc.Invoke(ctx, "/telepresence.connector.Connector/Status", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *connectorClient) CreateIntercept(ctx context.Context, in *CreateInterceptRequest, opts ...grpc.CallOption) (*InterceptResult, error) {
	out := new(InterceptResult)
	err := c.cc.Invoke(ctx, "/telepresence.connector.Connector/CreateIntercept", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *connectorClient) RemoveIntercept(ctx context.Context, in *manager.RemoveInterceptRequest2, opts ...grpc.CallOption) (*InterceptResult, error) {
	out := new(InterceptResult)
	err := c.cc.Invoke(ctx, "/telepresence.connector.Connector/RemoveIntercept", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *connectorClient) Uninstall(ctx context.Context, in *UninstallRequest, opts ...grpc.CallOption) (*UninstallResult, error) {
	out := new(UninstallResult)
	err := c.cc.Invoke(ctx, "/telepresence.connector.Connector/Uninstall", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *connectorClient) List(ctx context.Context, in *ListRequest, opts ...grpc.CallOption) (*WorkloadInfoSnapshot, error) {
	out := new(WorkloadInfoSnapshot)
	err := c.cc.Invoke(ctx, "/telepresence.connector.Connector/List", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *connectorClient) UserNotifications(ctx context.Context, in *empty.Empty, opts ...grpc.CallOption) (Connector_UserNotificationsClient, error) {
	stream, err := c.cc.NewStream(ctx, &_Connector_serviceDesc.Streams[0], "/telepresence.connector.Connector/UserNotifications", opts...)
	if err != nil {
		return nil, err
	}
	x := &connectorUserNotificationsClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type Connector_UserNotificationsClient interface {
	Recv() (*Notification, error)
	grpc.ClientStream
}

type connectorUserNotificationsClient struct {
	grpc.ClientStream
}

func (x *connectorUserNotificationsClient) Recv() (*Notification, error) {
	m := new(Notification)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *connectorClient) Login(ctx context.Context, in *LoginRequest, opts ...grpc.CallOption) (*LoginResult, error) {
	out := new(LoginResult)
	err := c.cc.Invoke(ctx, "/telepresence.connector.Connector/Login", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *connectorClient) Logout(ctx context.Context, in *empty.Empty, opts ...grpc.CallOption) (*empty.Empty, error) {
	out := new(empty.Empty)
	err := c.cc.Invoke(ctx, "/telepresence.connector.Connector/Logout", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *connectorClient) GetCloudUserInfo(ctx context.Context, in *UserInfoRequest, opts ...grpc.CallOption) (*UserInfo, error) {
	out := new(UserInfo)
	err := c.cc.Invoke(ctx, "/telepresence.connector.Connector/GetCloudUserInfo", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *connectorClient) GetCloudAPIKey(ctx context.Context, in *KeyRequest, opts ...grpc.CallOption) (*KeyData, error) {
	out := new(KeyData)
	err := c.cc.Invoke(ctx, "/telepresence.connector.Connector/GetCloudAPIKey", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *connectorClient) GetCloudLicense(ctx context.Context, in *LicenseRequest, opts ...grpc.CallOption) (*LicenseData, error) {
	out := new(LicenseData)
	err := c.cc.Invoke(ctx, "/telepresence.connector.Connector/GetCloudLicense", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *connectorClient) SetLogLevel(ctx context.Context, in *manager.LogLevelRequest, opts ...grpc.CallOption) (*empty.Empty, error) {
	out := new(empty.Empty)
	err := c.cc.Invoke(ctx, "/telepresence.connector.Connector/SetLogLevel", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *connectorClient) Quit(ctx context.Context, in *empty.Empty, opts ...grpc.CallOption) (*empty.Empty, error) {
	out := new(empty.Empty)
	err := c.cc.Invoke(ctx, "/telepresence.connector.Connector/Quit", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ConnectorServer is the server API for Connector service.
// All implementations must embed UnimplementedConnectorServer
// for forward compatibility
type ConnectorServer interface {
	// Returns version information from the Connector
	Version(context.Context, *empty.Empty) (*common.VersionInfo, error)
	// Connects to the cluster and connects the laptop's network (via
	// the daemon process) to the cluster's network.  A result code of
	// UNSPECIFIED indicates that the connection was successfully
	// initiated; if already connected, then either ALREADY_CONNECTED or
	// MUST_RESTART is returned, based on whether the current connection
	// is in agreement with the ConnectionRequest.
	Connect(context.Context, *ConnectRequest) (*ConnectInfo, error)
	// Status is much like Connect, except that it doesn't actually do
	// anything.  It's a dry-run.
	Status(context.Context, *ConnectRequest) (*ConnectInfo, error)
	// Adds an intercept to a workload.  Requires having already called
	// Connect.
	CreateIntercept(context.Context, *CreateInterceptRequest) (*InterceptResult, error)
	// Deactivates and removes an existent workload intercept.
	// Requires having already called Connect.
	RemoveIntercept(context.Context, *manager.RemoveInterceptRequest2) (*InterceptResult, error)
	// Uninstalls traffic-agents and traffic-manager from the cluster.
	// Requires having already called Connect.
	Uninstall(context.Context, *UninstallRequest) (*UninstallResult, error)
	// Returns a list of workloads and their current intercept status.
	// Requires having already called Connect.
	List(context.Context, *ListRequest) (*WorkloadInfoSnapshot, error)
	// Returns a stream of messages to display to the user.  Does NOT
	// require having called anything else first.
	UserNotifications(*empty.Empty, Connector_UserNotificationsServer) error
	Login(context.Context, *LoginRequest) (*LoginResult, error)
	// Returns an error with code=NotFound if not currently logged in.
	Logout(context.Context, *empty.Empty) (*empty.Empty, error)
	GetCloudUserInfo(context.Context, *UserInfoRequest) (*UserInfo, error)
	GetCloudAPIKey(context.Context, *KeyRequest) (*KeyData, error)
	GetCloudLicense(context.Context, *LicenseRequest) (*LicenseData, error)
	// SetLogLevel will temporarily set the log-level for the daemon for a duration that is determined by the request.
	SetLogLevel(context.Context, *manager.LogLevelRequest) (*empty.Empty, error)
	// Quits (terminates) the connector process.
	Quit(context.Context, *empty.Empty) (*empty.Empty, error)
	mustEmbedUnimplementedConnectorServer()
}

// UnimplementedConnectorServer must be embedded to have forward compatible implementations.
type UnimplementedConnectorServer struct {
}

func (UnimplementedConnectorServer) Version(context.Context, *empty.Empty) (*common.VersionInfo, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Version not implemented")
}
func (UnimplementedConnectorServer) Connect(context.Context, *ConnectRequest) (*ConnectInfo, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Connect not implemented")
}
func (UnimplementedConnectorServer) Status(context.Context, *ConnectRequest) (*ConnectInfo, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Status not implemented")
}
func (UnimplementedConnectorServer) CreateIntercept(context.Context, *CreateInterceptRequest) (*InterceptResult, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CreateIntercept not implemented")
}
func (UnimplementedConnectorServer) RemoveIntercept(context.Context, *manager.RemoveInterceptRequest2) (*InterceptResult, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RemoveIntercept not implemented")
}
func (UnimplementedConnectorServer) Uninstall(context.Context, *UninstallRequest) (*UninstallResult, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Uninstall not implemented")
}
func (UnimplementedConnectorServer) List(context.Context, *ListRequest) (*WorkloadInfoSnapshot, error) {
	return nil, status.Errorf(codes.Unimplemented, "method List not implemented")
}
func (UnimplementedConnectorServer) UserNotifications(*empty.Empty, Connector_UserNotificationsServer) error {
	return status.Errorf(codes.Unimplemented, "method UserNotifications not implemented")
}
func (UnimplementedConnectorServer) Login(context.Context, *LoginRequest) (*LoginResult, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Login not implemented")
}
func (UnimplementedConnectorServer) Logout(context.Context, *empty.Empty) (*empty.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Logout not implemented")
}
func (UnimplementedConnectorServer) GetCloudUserInfo(context.Context, *UserInfoRequest) (*UserInfo, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetCloudUserInfo not implemented")
}
func (UnimplementedConnectorServer) GetCloudAPIKey(context.Context, *KeyRequest) (*KeyData, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetCloudAPIKey not implemented")
}
func (UnimplementedConnectorServer) GetCloudLicense(context.Context, *LicenseRequest) (*LicenseData, error) {
	return nil, status.Errorf(codes.Unimplemented, "method GetCloudLicense not implemented")
}
func (UnimplementedConnectorServer) SetLogLevel(context.Context, *manager.LogLevelRequest) (*empty.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SetLogLevel not implemented")
}
func (UnimplementedConnectorServer) Quit(context.Context, *empty.Empty) (*empty.Empty, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Quit not implemented")
}
func (UnimplementedConnectorServer) mustEmbedUnimplementedConnectorServer() {}

// UnsafeConnectorServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to ConnectorServer will
// result in compilation errors.
type UnsafeConnectorServer interface {
	mustEmbedUnimplementedConnectorServer()
}

func RegisterConnectorServer(s grpc.ServiceRegistrar, srv ConnectorServer) {
	s.RegisterService(&_Connector_serviceDesc, srv)
}

func _Connector_Version_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(empty.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConnectorServer).Version(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/telepresence.connector.Connector/Version",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConnectorServer).Version(ctx, req.(*empty.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

func _Connector_Connect_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ConnectRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConnectorServer).Connect(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/telepresence.connector.Connector/Connect",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConnectorServer).Connect(ctx, req.(*ConnectRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Connector_Status_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ConnectRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConnectorServer).Status(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/telepresence.connector.Connector/Status",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConnectorServer).Status(ctx, req.(*ConnectRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Connector_CreateIntercept_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateInterceptRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConnectorServer).CreateIntercept(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/telepresence.connector.Connector/CreateIntercept",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConnectorServer).CreateIntercept(ctx, req.(*CreateInterceptRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Connector_RemoveIntercept_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(manager.RemoveInterceptRequest2)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConnectorServer).RemoveIntercept(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/telepresence.connector.Connector/RemoveIntercept",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConnectorServer).RemoveIntercept(ctx, req.(*manager.RemoveInterceptRequest2))
	}
	return interceptor(ctx, in, info, handler)
}

func _Connector_Uninstall_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UninstallRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConnectorServer).Uninstall(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/telepresence.connector.Connector/Uninstall",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConnectorServer).Uninstall(ctx, req.(*UninstallRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Connector_List_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConnectorServer).List(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/telepresence.connector.Connector/List",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConnectorServer).List(ctx, req.(*ListRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Connector_UserNotifications_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(empty.Empty)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(ConnectorServer).UserNotifications(m, &connectorUserNotificationsServer{stream})
}

type Connector_UserNotificationsServer interface {
	Send(*Notification) error
	grpc.ServerStream
}

type connectorUserNotificationsServer struct {
	grpc.ServerStream
}

func (x *connectorUserNotificationsServer) Send(m *Notification) error {
	return x.ServerStream.SendMsg(m)
}

func _Connector_Login_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(LoginRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConnectorServer).Login(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/telepresence.connector.Connector/Login",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConnectorServer).Login(ctx, req.(*LoginRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Connector_Logout_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(empty.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConnectorServer).Logout(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/telepresence.connector.Connector/Logout",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConnectorServer).Logout(ctx, req.(*empty.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

func _Connector_GetCloudUserInfo_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(UserInfoRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConnectorServer).GetCloudUserInfo(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/telepresence.connector.Connector/GetCloudUserInfo",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConnectorServer).GetCloudUserInfo(ctx, req.(*UserInfoRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Connector_GetCloudAPIKey_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(KeyRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConnectorServer).GetCloudAPIKey(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/telepresence.connector.Connector/GetCloudAPIKey",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConnectorServer).GetCloudAPIKey(ctx, req.(*KeyRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Connector_GetCloudLicense_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(LicenseRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConnectorServer).GetCloudLicense(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/telepresence.connector.Connector/GetCloudLicense",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConnectorServer).GetCloudLicense(ctx, req.(*LicenseRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Connector_SetLogLevel_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(manager.LogLevelRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConnectorServer).SetLogLevel(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/telepresence.connector.Connector/SetLogLevel",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConnectorServer).SetLogLevel(ctx, req.(*manager.LogLevelRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Connector_Quit_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(empty.Empty)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(ConnectorServer).Quit(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/telepresence.connector.Connector/Quit",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(ConnectorServer).Quit(ctx, req.(*empty.Empty))
	}
	return interceptor(ctx, in, info, handler)
}

var _Connector_serviceDesc = grpc.ServiceDesc{
	ServiceName: "telepresence.connector.Connector",
	HandlerType: (*ConnectorServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Version",
			Handler:    _Connector_Version_Handler,
		},
		{
			MethodName: "Connect",
			Handler:    _Connector_Connect_Handler,
		},
		{
			MethodName: "Status",
			Handler:    _Connector_Status_Handler,
		},
		{
			MethodName: "CreateIntercept",
			Handler:    _Connector_CreateIntercept_Handler,
		},
		{
			MethodName: "RemoveIntercept",
			Handler:    _Connector_RemoveIntercept_Handler,
		},
		{
			MethodName: "Uninstall",
			Handler:    _Connector_Uninstall_Handler,
		},
		{
			MethodName: "List",
			Handler:    _Connector_List_Handler,
		},
		{
			MethodName: "Login",
			Handler:    _Connector_Login_Handler,
		},
		{
			MethodName: "Logout",
			Handler:    _Connector_Logout_Handler,
		},
		{
			MethodName: "GetCloudUserInfo",
			Handler:    _Connector_GetCloudUserInfo_Handler,
		},
		{
			MethodName: "GetCloudAPIKey",
			Handler:    _Connector_GetCloudAPIKey_Handler,
		},
		{
			MethodName: "GetCloudLicense",
			Handler:    _Connector_GetCloudLicense_Handler,
		},
		{
			MethodName: "SetLogLevel",
			Handler:    _Connector_SetLogLevel_Handler,
		},
		{
			MethodName: "Quit",
			Handler:    _Connector_Quit_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "UserNotifications",
			Handler:       _Connector_UserNotifications_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "rpc/connector/connector.proto",
}
