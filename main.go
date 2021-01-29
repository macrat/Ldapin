package main

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"

	"github.com/alecthomas/kingpin"
	"github.com/gin-gonic/gin"
	"github.com/macrat/ldapin/api"
	"github.com/macrat/ldapin/config"
	"github.com/macrat/ldapin/ldap"
	"github.com/macrat/ldapin/page"
	"github.com/macrat/ldapin/token"
)

var (
	app = kingpin.New("ldapin", "The simple OpenID Provider for LDAP like an ActiveDirectory.")

	Issuer  = app.Flag("issuer", "Issuer URL.").Envar("LDAPIN_ISSUER").PlaceHolder(config.DefaultConfig.Issuer.String()).URL()
	Listen  = app.Flag("listen", "Listen address and port. In default, use the same port as the Issuer URL.").Envar("LDAPIN_LISTEN").TCP()
	SignKey = app.Flag("sign-key", "RSA private key for signing to token. If omit this, automate generate key for one time use.").Envar("LDAPIN_SIGN_KEY").PlaceHolder("FILE").File()

	AllowImplicitFlow = app.Flag("allow-implicit-flow", "Allow implicit/hybrid flow. It's may use for the SPA site or native application.").Envar("LDAPIN_ALLOW_IMPLICIT_FLOW").Bool()
	DisableClientAuth = app.Flag("disable-client-auth", "Allow use token endpoint without client authentication.").Envar("LDAPIN_DISABLE_CLIENT_AUTH").Bool()

	TLSCertFile = app.Flag("tls-cert", "Cert file for TLS encryption.").Envar("LDAPIN_TLS_CERT").PlaceHolder("FILE").ExistingFile()
	TLSKeyFile  = app.Flag("tls-key", "Key file for TLS encryption.").Envar("LDAPIN_TLS_KEY").PlaceHolder("FILE").ExistingFile()

	AuthzEndpoint    = app.Flag("authz-endpoint", "Path to authorization endpoint.").Envar("LDAPIN_AUTHz_ENDPOINT").PlaceHolder(config.DefaultConfig.Endpoints.Authz).String()
	TokenEndpoint    = app.Flag("token-endpoint", "Path to token endpoint.").Envar("LDAPIN_TOKEN_ENDPOINT").PlaceHolder(config.DefaultConfig.Endpoints.Token).String()
	UserinfoEndpoint = app.Flag("userinfo-endpoint", "Path to userinfo endpoint.").Envar("LDAPIN_USERINFO_ENDPOINT").PlaceHolder(config.DefaultConfig.Endpoints.Userinfo).String()
	JwksEndpoint     = app.Flag("jwks-uri", "Path to jwks uri.").Envar("LDAPIN_JWKS_URI").PlaceHolder(config.DefaultConfig.Endpoints.Jwks).String()

	CodeTTL    = app.Flag("code-ttl", "TTL for code.").Envar("LDAPIN_CODE_TTL").PlaceHolder("5m").String()
	TokenTTL   = app.Flag("token-ttl", "TTL for access_token and id_token.").Envar("LDAPIN_TOKEN_TTL").PlaceHolder("1d").String()
	RefreshTTL = app.Flag("refresh-ttl", "TTL for refresh_token. If set 0, refresh_token will not create.").Envar("LDAPIN_REFRESH_TTL").PlaceHolder("7d").String()
	SSOTTL     = app.Flag("sso-ttl", "TTL for single sign-on. If set 0, always ask the username and password to the end-user.").Envar("LDAPIN_SSO_TTL").PlaceHolder("14d").String()

	LdapAddress     = app.Flag("ldap", "URL of LDAP server like \"ldap://USER_DN:PASSWORD@ldap.example.com\".").Envar("LDAP_ADDRESS").PlaceHolder("ADDRESS").Required().URL()
	LdapBaseDN      = app.Flag("ldap-base-dn", "The base DN for search user account in LDAP like \"OU=somewhere,DC=example,DC=local\".").Envar("LDAP_BASE_DN").PlaceHolder("DN").Required().String() // TODO: make it automate set same OU as bind user if omit.
	LdapIDAttribute = app.Flag("ldap-id-attribute", "ID attribute name in LDAP.").Envar("LDAP_ID_ATTRIBUTE").Default("sAMAccountName").String()
	LdapDisableTLS  = app.Flag("ldap-disable-tls", "Disable use TLS when connecting to the LDAP server. THIS IS INSECURE.").Envar("LDAP_DISABLE_TLS").Bool()

	LoginPage = app.Flag("login-page", "Templte file for login page.").Envar("LDAPIN_LOGIN_PAGE").PlaceHolder("FILE").File()
	ErrorPage = app.Flag("error-page", "Templte file for error page.").Envar("LDAPIN_ERROR_PAGE").PlaceHolder("FILE").File()

	Config  = app.Flag("config", "Load options from YAML file.").Envar("LDAPIN_CONFIG").PlaceHolder("FILE").File()
	Verbose = app.Flag("verbose", "Enable debug mode.").Envar("LDAPIN_VERBOSE").Bool()
)

func DecideListenAddress(issuer *url.URL, listen *net.TCPAddr) string {
	if listen != nil {
		return listen.String()
	}

	if issuer.Port() != "" {
		return fmt.Sprintf(":%s", issuer.Port())
	}

	if issuer.Scheme == "https" {
		return ":443"
	}
	return ":80"
}

func main() {
	kingpin.MustParse(app.Parse(os.Args[1:]))

	var codeExpiresIn, tokenExpiresIn, refreshExpiresIn, ssoExpiresIn *config.Duration
	var err error
	if *CodeTTL != "" {
		codeExpiresIn, err = config.ParseDuration(*CodeTTL)
		app.FatalIfError(err, "--code-ttl")
	}
	if *TokenTTL != "" {
		tokenExpiresIn, err = config.ParseDuration(*TokenTTL)
		app.FatalIfError(err, "--token-ttl")
	}
	if *RefreshTTL != "" {
		refreshExpiresIn, err = config.ParseDuration(*RefreshTTL)
		app.FatalIfError(err, "--refresh-ttl")
	}
	if *SSOTTL != "" {
		ssoExpiresIn, err = config.ParseDuration(*SSOTTL)
		app.FatalIfError(err, "--sso-ttl")
	}

	if *TLSCertFile != "" && *TLSKeyFile == "" {
		app.Fatalf("--tls-key is required when set --tls-cert")
	} else if *TLSCertFile == "" && *TLSKeyFile != "" {
		app.Fatalf("--tls-cert is required when set --tls-key")
	}
	if *TLSCertFile != "" && *TLSKeyFile != "" && (*Issuer).Scheme != "https" {
		app.Fatalf("Please set https URL for --issuer when use TLS.")
	}

	if *Verbose {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	ldapUser := (*LdapAddress).User.Username()
	ldapPassword, _ := (*LdapAddress).User.Password()
	if ldapUser == "" && ldapPassword == "" {
		app.Fatalf("--ldap is must be has user and password information.")
		return
	}

	conf := config.DefaultConfig
	if *Config != nil {
		loaded, err := config.LoadConfig(*Config)
		app.FatalIfError(err, "failed to load config file")
		conf.Override(loaded)
	}
	conf.Override(&config.LdapinConfig{
		Issuer: (*config.URL)(*Issuer),
		Listen: (*config.TCPAddr)(*Listen),
		TTL: config.TTLConfig{
			Code:    codeExpiresIn,
			Token:   tokenExpiresIn,
			Refresh: refreshExpiresIn,
			SSO:     ssoExpiresIn,
		},
		Endpoints: config.EndpointConfig{
			Authz:    *AuthzEndpoint,
			Token:    *TokenEndpoint,
			Userinfo: *UserinfoEndpoint,
			Jwks:     *JwksEndpoint,
		},
		DisableClientAuth: *DisableClientAuth,
		AllowImplicitFlow: *AllowImplicitFlow,
	})
	addr := DecideListenAddress((*url.URL)(conf.Issuer), (*net.TCPAddr)(conf.Listen))

	if *conf.TTL.Code <= 0 {
		app.Fatalf("--code-ttl can't set 0 or less.")
	}
	if *conf.TTL.Token <= 0 {
		app.Fatalf("--token-ttl can't set 0 or less.")
	}

	fmt.Printf("OpenID Provider \"%s\" started on %s\n", conf.Issuer, addr)
	fmt.Println()

	if conf.Issuer.Scheme == "http" {
		fmt.Fprintln(os.Stderr, "DANGER  Serve OAuth2/OpenID service over no encrypted HTTP.")
		fmt.Fprintln(os.Stderr, "        An attacker can peek or rewrite user credentials, profile, or authorization.")
		fmt.Fprintln(os.Stderr, "        Please set HTTPS URL to --issuer option.")
		fmt.Fprintln(os.Stderr, "        And, you can enable TLS by --tls-cert and --tls-key options.")
		fmt.Fprintln(os.Stderr, "")
	}

	if (*LdapAddress).Scheme == "ldap" && *LdapDisableTLS {
		fmt.Fprintln(os.Stderr, "DANGER  Communication with LDAP server won't encryption.")
		fmt.Fprintln(os.Stderr, "        An attacker in your network can peek at user credentials or profile.")
		fmt.Fprintln(os.Stderr, "        Please consider removing --ldap-disable-tls option.")
		fmt.Fprintln(os.Stderr, "")
	}

	if len(conf.Clients) == 0 && !conf.DisableClientAuth {
		fmt.Fprintln(os.Stderr, "WARNING  No client is registered in the config file.")
		fmt.Fprintln(os.Stderr, "         So, no client can use this provider.")
		fmt.Fprintln(os.Stderr, "         Please consider register clients or use --disable-client-auth option.")
		fmt.Fprintln(os.Stderr, "")
	}

	if !conf.AllowImplicitFlow {
		fmt.Fprintln(os.Stderr, "NOTE  Implicit flow is disallowed.")
		fmt.Fprintln(os.Stderr, "      Perhaps you have to allow this if used by SPA site.")
		fmt.Fprintln(os.Stderr, "      You can allow this with --allow-implicit-flow option.")
		fmt.Fprintln(os.Stderr, "")
	}

	connector := ldap.SimpleConnector{
		ServerURL:   *LdapAddress,
		User:        ldapUser,
		Password:    ldapPassword,
		IDAttribute: *LdapIDAttribute,
		BaseDN:      *LdapBaseDN,
		DisableTLS:  *LdapDisableTLS,
	}
	_, err = connector.Connect()
	app.FatalIfError(err, "failed to connect LDAP server")

	var tokenManager token.Manager
	if *SignKey != nil {
		tokenManager, err = token.NewManagerFromFile(*SignKey)
		app.FatalIfError(err, "failed to read private key for sign")
	} else {
		tokenManager, err = token.GenerateManager()
		app.FatalIfError(err, "failed to generate private key for sign")
	}

	api := &api.LdapinAPI{
		Connector:    connector,
		TokenManager: tokenManager,
		Config:       conf,
	}

	tmpl, err := page.Load(*LoginPage, *ErrorPage)
	app.FatalIfError(err, "failed to load template")
	router.SetHTMLTemplate(tmpl)

	router.Use(func(c *gin.Context) {
		fmt.Println(c.Request.URL)
		c.Header("X-Frame-Options", "DENY")
		c.Header("Content-Security-Policy", "frame-ancestors 'none'")
	})

	api.SetRoutes(router)
	api.SetErrorRoutes(router)

	server := &http.Server{
		Addr:    addr,
		Handler: HTTPCompressor(router),
	}
	if *TLSCertFile != "" {
		err = server.ListenAndServeTLS(*TLSCertFile, *TLSKeyFile)
		app.FatalIfError(err, "failed to start server")
	} else {
		err = server.ListenAndServe()
		app.FatalIfError(err, "failed to start server")
	}
}
