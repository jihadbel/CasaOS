package route

import (
	"crypto/ecdsa"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/IceWhaleTech/CasaOS/codegen"
	"github.com/IceWhaleTech/CasaOS/pkg/config"
	"github.com/IceWhaleTech/CasaOS/pkg/localauth"
	v1Route "github.com/IceWhaleTech/CasaOS/route/v1"

	"github.com/IceWhaleTech/CasaOS-Common/external"
	"github.com/IceWhaleTech/CasaOS-Common/utils/jwt"
	v2Route "github.com/IceWhaleTech/CasaOS/route/v2"
	"github.com/deepmap/oapi-codegen/pkg/middleware"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/labstack/echo/v4"
	echo_middleware "github.com/labstack/echo/v4/middleware"
)

var (
	_swagger *openapi3.T

	V2APIPath  string
	V2DocPath  string
	V3FilePath string
)

func init() {
	swagger, err := codegen.GetSwagger()
	if err != nil {
		panic(err)
	}

	_swagger = swagger

	u, err := url.Parse(_swagger.Servers[0].URL)
	if err != nil {
		panic(err)
	}

	V2APIPath = strings.TrimRight(u.Path, "/")
	V2DocPath = "/doc" + V2APIPath
	V3FilePath = "/v3/file"
}

func InitV2Router() http.Handler {
	appManagement := v2Route.NewCasaOS()

	e := echo.New()

	e.Use((echo_middleware.CORSWithConfig(echo_middleware.CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{echo.POST, echo.GET, echo.OPTIONS, echo.PUT, echo.DELETE},
		AllowHeaders:     []string{echo.HeaderAuthorization, echo.HeaderContentLength, echo.HeaderXCSRFToken, echo.HeaderContentType, echo.HeaderAccessControlAllowOrigin, echo.HeaderAccessControlAllowHeaders, echo.HeaderAccessControlAllowMethods, echo.HeaderConnection, echo.HeaderOrigin, echo.HeaderXRequestedWith},
		ExposeHeaders:    []string{echo.HeaderContentLength, echo.HeaderAccessControlAllowOrigin, echo.HeaderAccessControlAllowHeaders},
		MaxAge:           172800,
		AllowCredentials: true,
	})))

	e.Use(echo_middleware.Recover())
	e.Use(echo_middleware.Gzip())

	e.Use(echo_middleware.LoggerWithConfig(echo_middleware.LoggerConfig{Format: "${method} ${path} ${status}\n"}))

	e.Use(echo_middleware.JWTWithConfig(echo_middleware.JWTConfig{
		Skipper: func(c echo.Context) bool {
			return localauth.Authenticated(c.Request())
		},
		ParseTokenFunc: func(token string, c echo.Context) (interface{}, error) {
			valid, claims, err := jwt.Validate(token, func() (*ecdsa.PublicKey, error) { return external.GetPublicKey(config.CommonInfo.RuntimePath) })
			if err != nil || !valid {
				return nil, echo.ErrUnauthorized
			}
			c.Request().Header.Set("user_id", strconv.Itoa(claims.ID))

			return claims, nil
		},
		TokenLookupFuncs: []echo_middleware.ValuesExtractor{
			func(ctx echo.Context) ([]string, error) {
				if len(ctx.Request().Header.Get(echo.HeaderAuthorization)) > 0 {
					return []string{ctx.Request().Header.Get(echo.HeaderAuthorization)}, nil
				}
				return []string{ctx.QueryParam("token")}, nil
			},
		},
	}))

	// e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
	// 	return func(c echo.Context) error {
	// 		switch c.Request().Header.Get(echo.HeaderContentType) {
	// 		case common.MIMEApplicationYAML: // in case request contains a compose content in YAML
	// 			return middleware.OapiRequestValidatorWithOptions(_swagger, &middleware.Options{
	// 				Options: openapi3filter.Options{
	// 					AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
	// 					// ExcludeRequestBody:  true,
	// 					// ExcludeResponseBody: true,
	// 				},
	// 			})(next)(c)

	// 		default:
	// 			return middleware.OapiRequestValidatorWithOptions(_swagger, &middleware.Options{
	// 				Options: openapi3filter.Options{
	// 					AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
	// 				},
	// 			})(next)(c)
	// 		}
	// 	}
	// })

	e.Use(middleware.OapiRequestValidatorWithOptions(_swagger, &middleware.Options{
		Skipper: func(c echo.Context) bool {
			// jump validate when upload file
			// because file upload can't pass validate
			// issue: https://github.com/deepmap/oapi-codegen/issues/514
			return strings.Contains(c.Request().Header.Get(echo.HeaderContentType), "multipart/form-data")
		},
		Options: openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
	}))

	codegen.RegisterHandlersWithBaseURL(e, appManagement, V2APIPath)

	return e
}

func InitV2DocRouter(docHTML string, docYAML string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == V2DocPath {
			if _, err := w.Write([]byte(docHTML)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
			return
		}

		if r.URL.Path == V2DocPath+"/openapi.yaml" {
			if _, err := w.Write([]byte(docYAML)); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
		}
	})
}

func InitFile() http.Handler {
	e := echo.New()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !localauth.Authenticated(r) {
			token := r.URL.Query().Get("token")
			if len(token) == 0 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"message": "token not found"}`))
				return
			}

			valid, _, errs := jwt.Validate(token, func() (*ecdsa.PublicKey, error) { return external.GetPublicKey(config.CommonInfo.RuntimePath) })
			if errs != nil || !valid {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"message": "validation failure"}`))
				return
			}
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx := e.NewContext(r, w)
		if err := v1Route.GetDownloadAttachment(ctx); err != nil {
			e.HTTPErrorHandler(err, ctx)
		}
	})
}

// InitDir uses the same authenticated, bounded export as V1.
func InitDir() http.Handler {
	e := echo.New()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !localauth.Authenticated(r) {
			valid, _, err := jwt.Validate(r.URL.Query().Get("token"), func() (*ecdsa.PublicKey, error) { return external.GetPublicKey(config.CommonInfo.RuntimePath) })
			if err != nil || !valid {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx := e.NewContext(r, w)
		if err := v1Route.GetDownloadFile(ctx); err != nil {
			e.HTTPErrorHandler(err, ctx)
		}
	})
}
