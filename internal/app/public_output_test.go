package app

import "testing"

func TestRedactPublicDiagnosticCredentialShapes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "authorization bearer",
			in:   "upstream rejected Authorization: Bearer bearer-secret-sentinel",
			want: "upstream rejected Authorization: [REDACTED]",
		},
		{
			name: "authorization basic",
			in:   "upstream rejected Authorization: Basic basic-secret-sentinel",
			want: "upstream rejected Authorization: [REDACTED]",
		},
		{
			name: "authorization digest parameters",
			in:   `upstream rejected Authorization: Digest username="reader", response="digest-secret", nonce="nonce-secret"`,
			want: "upstream rejected Authorization: [REDACTED]",
		},
		{
			name: "authorization aws scheme",
			in:   "upstream rejected Authorization: AWS4-HMAC-SHA256 Credential=access-key-secret, Signature=signature-secret",
			want: "upstream rejected Authorization: [REDACTED]",
		},
		{
			name: "standalone bearer",
			in:   "upstream rejected Bearer standalone-secret-sentinel",
			want: "upstream rejected Bearer [REDACTED]",
		},
		{
			name: "double quoted assignment with spaces",
			in:   `connection failed password="double quoted secret"`,
			want: "connection failed password=[REDACTED]",
		},
		{
			name: "single quoted assignment with spaces",
			in:   "connection failed password='single quoted secret'",
			want: "connection failed password=[REDACTED]",
		},
		{
			name: "double quoted assignment with escaped quote",
			in:   `connection failed password="prefix \" escaped-secret suffix"`,
			want: "connection failed password=[REDACTED]",
		},
		{
			name: "single quoted assignment with escaped quote",
			in:   `connection failed password='prefix \' escaped-secret suffix'`,
			want: "connection failed password=[REDACTED]",
		},
		{
			name: "unterminated quoted assignment",
			in:   `connection failed password="assignment-secret with spaces`,
			want: "connection failed password=[REDACTED]",
		},
		{
			name: "json string",
			in:   `driver reply {"api_key":"json secret with spaces","result":"retry"}`,
			want: `driver reply {"api_key":"[REDACTED]","result":"retry"}`,
		},
		{
			name: "json string with escaped quote",
			in:   `driver reply {"api_key":"prefix \" json-secret suffix","result":"retry"}`,
			want: `driver reply {"api_key":"[REDACTED]","result":"retry"}`,
		},
		{
			name: "unterminated quoted json",
			in:   `driver reply {"api_key":"json-secret with spaces`,
			want: `driver reply {"api_key":"[REDACTED]"`,
		},
		{
			name: "semicolon dsn",
			in:   "connect Server=db.example;Password=semicolon-secret;Database=app",
			want: "connect Server=db.example;Password=[REDACTED];Database=app",
		},
		{
			name: "uri username password userinfo",
			in:   "connect sqlserver://reader:uri-secret@db.example:1433/app",
			want: "connect sqlserver://[REDACTED]@db.example:1433/app",
		},
		{
			name: "uri empty username userinfo",
			in:   "connect postgres://:uri-secret@db.example/app",
			want: "connect postgres://[REDACTED]@db.example/app",
		},
		{
			name: "uri token only userinfo",
			in:   "connect https://uri-token-secret@api.example/v1",
			want: "connect https://[REDACTED]@api.example/v1",
		},
		{
			name: "ordinary URL unchanged",
			in:   "connect https://api.example/v1/status",
			want: "connect https://api.example/v1/status",
		},
		{
			name: "api key and token assignments",
			in:   "api_key=api-key-secret token: token-secret",
			want: "api_key=[REDACTED] token:[REDACTED]",
		},
		{
			name: "quoted credential flag",
			in:   `retry --password "flag secret with spaces" after connection failure`,
			want: "retry --password [REDACTED] after connection failure",
		},
		{
			name: "quoted credential flag with escaped quote",
			in:   `retry --password "prefix \" flag-secret suffix" after connection failure`,
			want: "retry --password [REDACTED] after connection failure",
		},
		{
			name: "unterminated quoted credential flag",
			in:   `retry --password "flag-secret with spaces`,
			want: "retry --password [REDACTED]",
		},
		{
			name: "quoted bearer with escaped quote",
			in:   `upstream rejected Bearer "prefix \" bearer-secret suffix"`,
			want: "upstream rejected Bearer [REDACTED]",
		},
		{
			name: "unterminated quoted bearer",
			in:   `upstream rejected Bearer "bearer-secret with spaces`,
			want: "upstream rejected Bearer [REDACTED]",
		},
		{
			name: "ordinary prose and table names",
			in:   "password authentication failed for bearer_counts; use the Bearer authentication scheme",
			want: "password authentication failed for bearer_counts; use the Bearer authentication scheme",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := redactPublicDiagnostic(test.in); got != test.want {
				t.Fatalf("redactPublicDiagnostic(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}
