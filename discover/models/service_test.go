package models

import (
	"testing"
)

func TestServiceExternal(t *testing.T) {
	type args struct {
		svc *Service
	}

	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "success - external name empty",
			args: args{svc: &Service{}},
			want: false,
		},
		{
			name: "success - external name populated",
			args: args{svc: &Service{ExternalName: "external-name"}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.args.svc.External(); got != tt.want {
				t.Errorf("Service.External() = %v, expected = %v", got, tt.want)
			}
		})
	}
}

func TestPortListGet(t *testing.T) {
	p1 := &Port{Name: "testport 1", Port: 8080}
	p2 := &Port{Name: "testport 2", Port: 3000}
	p3 := &Port{Name: "testport 3", Port: 8443}

	plist := PortList{p1, p2, p3}

	type args struct {
		portname string
	}

	tests := []struct {
		name     string
		args     args
		want     *Port
		wantBool bool
	}{
		{
			name:     "success - port exists - testport 1",
			args:     args{portname: "testport 1"},
			want:     p1,
			wantBool: true,
		},
		{
			name:     "success - port exists - testport 2",
			args:     args{portname: "testport 2"},
			want:     p2,
			wantBool: true,
		},
		{
			name:     "success - port exists - testport 3",
			args:     args{portname: "testport 3"},
			want:     p3,
			wantBool: true,
		},
		{
			name:     "success - port does not exist",
			args:     args{portname: "non-existent portname"},
			want:     nil,
			wantBool: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotbool := plist.get(tt.args.portname)
			if gotbool != tt.wantBool {
				t.Errorf("PortList.get() bool = %v, expected = %v", gotbool, tt.wantBool)

				return
			}

			if got != tt.want {
				t.Errorf("PortList.get() = %v, expected = %v", got, tt.want)
			}
		})
	}
}

func TestHTTPProtocol(t *testing.T) {
	if ProtocolTCP.IsHTTP() {
		t.Errorf("TCP is not HTTP protocol")
	}

	if !ProtocolGRPC.IsHTTP() {
		t.Errorf("gRPC is HTTP protocol")
	}
}

func TestConvertCaseInsensitiveStringToProtocol(t *testing.T) {
	testPairs := []struct {
		name string
		out  Protocol
	}{
		{"tcp", ProtocolTCP},
		{"http", ProtocolHTTP},
		{"HTTP", ProtocolHTTP},
		{"Http", ProtocolHTTP},
		{"https", ProtocolHTTPS},
		{"http2", ProtocolHTTP2},
		{"grpc", ProtocolGRPC},
		{"", ProtocolUnsupported},
		{"SMTP", ProtocolUnsupported},
	}

	for _, testPair := range testPairs {
		out := ConvertCaseInsensitiveStringToProtocol(testPair.name)
		if out != testPair.out {
			t.Errorf("ConvertCaseInsensitiveStringToProtocol(%q) => %q, want %q", testPair.name, out, testPair.out)
		}
	}
}
