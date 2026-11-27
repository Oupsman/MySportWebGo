package webserver

import (
	"MySportWeb/internal/pkg/apicontrollers"
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/controllers"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/pictures"
	"MySportWeb/internal/pkg/vars"
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog"
)

// The largest request bodies accepted
const (
	// DefaultBodyLimit is the one of the JSON requests
	DefaultBodyLimit = 1 << 20
	// FitBodyLimit is the one of the FIT file uploads: a day long recording is about 10 MB. The whole file
	// is decoded in memory.
	FitBodyLimit = 25 << 20
	// PictureBodyLimit is the one of the equipment pictures, pictures.MaxUploadSize and the multipart form
	PictureBodyLimit = pictures.MaxUploadSize + 1<<20
	// HealthImportBodyLimit is the one of the health imports, 5000 measures
	HealthImportBodyLimit = 4 << 20
)

// The attempts accepted per IP: every login and registration hashes a password, about a second of CPU
const (
	LoginsPerMinute      = 10
	RegistrationsPerHour = 5
)

// DEAD CODE: not used anywhere, commented out to keep it out of the build (kept for reference).
// type WebServer struct {
// 	Router *gin.Engine
// }

func AppHandler(App *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("App", App)
		c.Next()
	}
}

// RunHttp serves the API until ctx is done, then waits for the requests running (see serve)
func RunHttp(ctx context.Context, listenAddr string, App *app.App) error {

	// release mode unless GIN_MODE says otherwise (debug for development): debug mode logs the routes and
	// its warnings at each start
	if os.Getenv(gin.EnvGinMode) == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	httpRouter := gin.Default()
	// first: the requests the middlewares below refuse are counted too
	httpRouter.Use(instrument)
	// the IP of the client (rate limits, logs) comes from X-Forwarded-For only behind these proxies: by
	// default gin trusts it from anyone, a client could choose its IP
	if err := httpRouter.SetTrustedProxies(vars.TrustedProxies); err != nil {
		return err
	}
	httpRouter.Use(AppHandler(App))
	// the multipart files above this are written to temporary files instead of memory
	httpRouter.MaxMultipartMemory = 8 << 20
	httpRouter.Use(middlewares.LimitBody(DefaultBodyLimit, map[string]int64{
		"/api/v1/activities/upload":     FitBodyLimit,
		"/api/v1/activity":              FitBodyLimit,
		"/api/v1/equipment/:id/picture": PictureBodyLimit,
		"/api/v1/healthdatas/import":    HealthImportBodyLimit,
		"/api/v1/user/import":           vars.ImportMaxSize,
	}))

	// the cors middleware refuses other origins silently (403, or a missing header in the browser)
	httpRouter.Use(func(c *gin.Context) {
		if origin := c.GetHeader("Origin"); origin != "" && origin != vars.Domain {
			App.Logger.Warn().Str("origin", origin).Str("allowed_origin", vars.Domain).
				Str("method", c.Request.Method).Str("path", c.Request.URL.Path).
				Msg("CORS: request from an origin that is not allowed, the browser will block it (set DOMAIN in .env)")
		}
		c.Next()
	})
	httpRouter.Use(cors.New(cors.Config{
		AllowOrigins:  []string{vars.Domain},
		AllowMethods:  []string{"GET", "OPTIONS", "POST", "PUT", "DELETE"},
		AllowHeaders:  []string{"Origin", "Authorization", "Content-Type", "X-TOTP-Code"},
		ExposeHeaders: []string{"Content-Length", "Retry-After"},
		// the session cookie, for the frontend of the development server (another port: another origin)
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))
	// the session is a cookie: a page of another origin must not change anything with it (CSRF)
	httpRouter.Use(middlewares.CrossOriginProtection())

	httpRouter.GET("/", apicontrollers.HealthCheck)
	// httpRouter.GET("/login", controllers.Login)
	// httpRouter.GET("/signup", controllers.SignUp)

	// only the thumbnails and the equipment pictures, never the FIT files nor a directory listing; to whom
	// may see them, from the session cookie
	httpRouter.GET("/MEDIA/*path", middlewares.OptionalUser(), apicontrollers.ServeMedia)
	httpRouter.HEAD("/MEDIA/*path", middlewares.OptionalUser(), apicontrollers.ServeMedia)

	apiv1 := httpRouter.Group("/api/v1")
	{
		apiv1.GET("/", middlewares.IsAuthorized(), controllers.IsAuthenticated)
	}

	apiv1.GET("/healthcheck", apicontrollers.HealthCheck)
	// how to register (REGISTRATION, the setup of the admin): before any login
	apiv1.GET("/instance", apicontrollers.Instance)
	// Admins: sessions of the admins only
	apiv1.GET("/admin/invitations", middlewares.IsAdmin(), apicontrollers.GetInvitations)
	apiv1.POST("/admin/invitations", middlewares.IsAdmin(), apicontrollers.CreateInvitation)
	apiv1.DELETE("/admin/invitations/:id", middlewares.IsAdmin(), apicontrollers.RevokeInvitation)
	// the changes of the users also ask a code of the authenticator, each time (apicontrollers.RequireTOTP)
	apiv1.GET("/admin/users", middlewares.IsAdmin(), apicontrollers.GetUsers)
	// the state of the instance, from the metrics read in the process: no Prometheus needed
	apiv1.GET("/admin/status", middlewares.IsAdmin(), apicontrollers.GetStatus)
	// what the users do and the notifications they get (never their message)
	apiv1.GET("/admin/events", middlewares.IsAdmin(), apicontrollers.GetUserEvents)
	apiv1.GET("/admin/notifications", middlewares.IsAdmin(), apicontrollers.GetNotificationLogs)
	apiv1.POST("/admin/email/test", middlewares.IsAdmin(), apicontrollers.SendTestEmail)
	apiv1.POST("/admin/users/:id/role", middlewares.IsAdmin(), apicontrollers.SetUserRole)
	apiv1.POST("/admin/users/:id/disable", middlewares.IsAdmin(), apicontrollers.DisableUser)
	apiv1.POST("/admin/users/:id/enable", middlewares.IsAdmin(), apicontrollers.EnableUser)
	apiv1.POST("/admin/users/:id/password-reset", middlewares.IsAdmin(), apicontrollers.CreatePasswordReset)
	apiv1.DELETE("/admin/users/:id", middlewares.IsAdmin(), apicontrollers.AdminDeleteUser)
	// their own too: the quota isn't a privilege
	apiv1.POST("/admin/users/:id/quota", middlewares.IsAdmin(), apicontrollers.SetUserQuota)
	// Users
	// per IP; the logins are also limited per username, see apicontrollers.Login
	apiv1.POST("/user/login", middlewares.RateLimit(middlewares.NewRateLimiter(LoginsPerMinute, time.Minute)),
		apicontrollers.Login)
	apiv1.POST("/user/password-forgot", middlewares.RateLimit(middlewares.NewRateLimiter(LoginsPerMinute, time.Minute)),
		apicontrollers.ForgotPassword)
	apiv1.POST("/user/email/verify", middlewares.RateLimit(middlewares.NewRateLimiter(LoginsPerMinute, time.Minute)),
		apicontrollers.VerifyEmail)
	apiv1.GET("/user/email", middlewares.IsAuthorized(), apicontrollers.EmailStatus)
	apiv1.POST("/user/email/resend", middlewares.IsAuthorized(), apicontrollers.ResendVerification)
	apiv1.GET("/notifications/email", middlewares.IsAuthorized(), apicontrollers.EmailNotifications)
	apiv1.POST("/notifications/email", middlewares.IsAuthorized(), apicontrollers.SetEmailNotifications)
	apiv1.GET("/notifications/social", middlewares.IsAuthorized(), apicontrollers.SocialNotifications)
	apiv1.POST("/notifications/social", middlewares.IsAuthorized(), apicontrollers.SetSocialNotifications)
	apiv1.POST("/user/password-reset", middlewares.RateLimit(middlewares.NewRateLimiter(LoginsPerMinute, time.Minute)),
		apicontrollers.ResetPassword)
	apiv1.POST("/user/login/2fa", middlewares.RateLimit(middlewares.NewRateLimiter(LoginsPerMinute, time.Minute)),
		apicontrollers.LoginSecondFactor)
	// your data: sessions only
	apiv1.GET("/user/export", middlewares.IsAuthorized(), apicontrollers.ExportAccount)
	apiv1.POST("/user/import", middlewares.IsAuthorized(), apicontrollers.ImportAccount)
	apiv1.GET("/user/import", middlewares.IsAuthorized(), apicontrollers.GetAccountImport)
	apiv1.DELETE("/user/import", middlewares.IsAuthorized(), apicontrollers.CancelAccountImport)
	apiv1.POST("/user/delete", middlewares.IsAuthorized(), apicontrollers.DeleteAccount)
	apiv1.GET("/user/2fa", middlewares.IsAuthorized(), apicontrollers.TwoFactorStatus)
	apiv1.POST("/user/2fa/setup", middlewares.IsAuthorized(), apicontrollers.SetupTwoFactor)
	apiv1.POST("/user/2fa/enable", middlewares.IsAuthorized(), apicontrollers.EnableTwoFactor)
	apiv1.POST("/user/2fa/disable", middlewares.IsAuthorized(), apicontrollers.DisableTwoFactor)
	apiv1.POST("/user/2fa/recovery", middlewares.IsAuthorized(), apicontrollers.NewRecoveryCodes)
	apiv1.POST("/user/register", middlewares.RateLimit(middlewares.NewRateLimiter(RegistrationsPerHour, time.Hour)),
		apicontrollers.Register)
	// Routes given permissions are also open to device tokens holding them, the others to sessions only
	// a POST: a GET would let any page log the user out (an image pointing to it). Without authentication,
	// it clears the cookie of an ended session too.
	apiv1.POST("/user/logout", apicontrollers.Logout)
	// who is logged in: the web application can't read its cookie
	apiv1.GET("/user/session", middlewares.IsAuthorized(), apicontrollers.GetSession)
	apiv1.GET("/user/dashboard", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.Dashboard)
	// the training goals and their progress: read with the activities, set by sessions only
	apiv1.GET("/user/goals", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.GetGoals)
	apiv1.POST("/user/goals", middlewares.IsAuthorized(), apicontrollers.CreateGoal)
	apiv1.POST("/user/goals/:id", middlewares.IsAuthorized(), apicontrollers.UpdateGoal)
	apiv1.DELETE("/user/goals/:id", middlewares.IsAuthorized(), apicontrollers.DeleteGoal)
	// the workplaces: an activity starting or ending at one is a commute
	apiv1.GET("/user/workplaces", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.GetWorkplaces)
	apiv1.POST("/user/workplaces", middlewares.IsAuthorized(), apicontrollers.CreateWorkplace)
	apiv1.POST("/user/workplaces/:id", middlewares.IsAuthorized(), apicontrollers.UpdateWorkplace)
	apiv1.DELETE("/user/workplaces/:id", middlewares.IsAuthorized(), apicontrollers.DeleteWorkplace)
	apiv1.POST("/user/workplaces/:id/apply", middlewares.IsAuthorized(), apicontrollers.ApplyWorkplace)
	// how the user arranged their dashboard: sessions only
	apiv1.GET("/user/dashboard/layout", middlewares.IsAuthorized(), apicontrollers.GetDashboardLayout)
	apiv1.PUT("/user/dashboard/layout", middlewares.IsAuthorized(), apicontrollers.SetDashboardLayout)
	apiv1.GET("/user/hrzones", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.HrZones)
	apiv1.GET("/user/powerzones", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.PowerZones)
	apiv1.GET("/user/ftp", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.FTP)
	apiv1.GET("/user/powercurve", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.PowerCurve)
	// the traces of all the activities: sessions only
	apiv1.GET("/user/heatmap", middlewares.IsAuthorized(), apicontrollers.Heatmap)
	// the readiness reads the resting heart rates of the health data too
	apiv1.GET("/user/readiness", middlewares.IsAuthorized(models.ActivitiesRead, models.HealthDatasRead), apicontrollers.GetReadiness)
	apiv1.GET("/user/calendar", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.Calendar)
	apiv1.GET("/user/records", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.Records)
	apiv1.GET("/user/sports", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.UserSports)
	apiv1.GET("/user", middlewares.IsAuthorized(), apicontrollers.GetUser)
	// the space their files take, and their quota
	apiv1.GET("/user/storage", middlewares.IsAuthorized(), apicontrollers.GetStorage)
	apiv1.POST("/user", middlewares.IsAuthorized(), apicontrollers.UpdateUser)

	// browser notifications (web push), from a session only
	apiv1.GET("/notifications/push", middlewares.IsAuthorized(), apicontrollers.GetPush)
	apiv1.POST("/notifications/push", middlewares.IsAuthorized(), apicontrollers.Subscribe)
	apiv1.DELETE("/notifications/push/:id", middlewares.IsAuthorized(), apicontrollers.Unsubscribe)
	apiv1.POST("/notifications/push/test", middlewares.IsAuthorized(), apicontrollers.TestPush)

	// device tokens, managed from a session only: a device token can't create nor extend tokens
	apiv1.GET("/user/tokens", middlewares.IsAuthorized(), apicontrollers.GetDeviceTokens)
	apiv1.POST("/user/tokens", middlewares.IsAuthorized(), apicontrollers.CreateDeviceToken)
	apiv1.DELETE("/user/tokens/:id", middlewares.IsAuthorized(), apicontrollers.DeleteDeviceToken)
	// the device token of the request, open to any device token: the applications check theirs
	apiv1.GET("/token", middlewares.IsDeviceToken(), apicontrollers.GetCurrentDeviceToken)

	// Activities
	// the browsers' ad blockers refuse /api/v1/activity (EasyPrivacy: "/api/v1/activity|$~third-party,xmlhttprequest"):
	// the frontend uploads to /activities/upload, /activity stays for the API clients (device tokens)
	apiv1.POST("/activities/upload", middlewares.IsAuthorized(models.ActivitiesCreate), apicontrollers.UploadActivity)
	apiv1.POST("/activity", middlewares.IsAuthorized(models.ActivitiesCreate), apicontrollers.UploadActivity)
	apiv1.GET("/activity/list/:start/:count", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.ListActivities)
	apiv1.GET("/activity/search", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.SearchActivities)
	apiv1.GET("/activity/duplicates", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.FindDuplicates)
	apiv1.GET("/activity/:id", middlewares.OptionalUser(models.ActivitiesRead), apicontrollers.GetActivity)
	apiv1.GET("/activity/:id/similar", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.GetSimilarActivities)
	apiv1.DELETE("/activity/:id", middlewares.IsAuthorized(models.ActivitiesDelete), apicontrollers.DeleteActivity)
	apiv1.POST("/activity/:id", middlewares.IsAuthorized(models.ActivitiesWrite), apicontrollers.UpdateActivity)
	apiv1.POST("/activity/:id/trim", middlewares.IsAuthorized(models.ActivitiesWrite), apicontrollers.TrimActivity)
	apiv1.POST("/activity/:id/title", middlewares.IsAuthorized(models.ActivitiesWrite), apicontrollers.SetActivityTitle)

	// segments: stretches of a ride, the passes of the activities on them ranked
	apiv1.GET("/segments", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.GetSegments)
	apiv1.POST("/segments", middlewares.IsAuthorized(models.ActivitiesWrite), apicontrollers.CreateSegment)
	apiv1.GET("/segments/:id", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.GetSegment)
	apiv1.POST("/segments/:id", middlewares.IsAuthorized(models.ActivitiesWrite), apicontrollers.RenameSegment)
	apiv1.DELETE("/segments/:id", middlewares.IsAuthorized(models.ActivitiesWrite), apicontrollers.DeleteSegment)
	apiv1.GET("/activity/:id/segments", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.GetActivitySegments)
	// what it achieved: new bests, podiums of segments, goals completed
	apiv1.GET("/activity/:id/highlights", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.GetActivityHighlights)
	// the cheers and the comments: for the sessions of the web application only, never the device tokens
	apiv1.GET("/activity/:id/social", middlewares.IsAuthorized(), apicontrollers.GetActivitySocial)
	apiv1.PUT("/activity/:id/cheer", middlewares.IsAuthorized(), apicontrollers.CheerActivity)
	apiv1.DELETE("/activity/:id/cheer", middlewares.IsAuthorized(), apicontrollers.UncheerActivity)
	apiv1.POST("/activity/:id/comments", middlewares.IsAuthorized(), apicontrollers.CreateComment)
	apiv1.POST("/activity/:id/comments/:comment", middlewares.IsAuthorized(), apicontrollers.UpdateComment)
	apiv1.DELETE("/activity/:id/comments/:comment", middlewares.IsAuthorized(), apicontrollers.DeleteComment)
	// the follows and the feed: for the sessions only too
	apiv1.GET("/users/search", middlewares.IsAuthorized(), apicontrollers.SearchUsers)
	apiv1.GET("/users/:uuid", middlewares.IsAuthorized(), apicontrollers.GetProfile)
	apiv1.GET("/users/:uuid/activities", middlewares.IsAuthorized(), apicontrollers.GetProfileActivities)
	apiv1.POST("/users/:uuid/follow", middlewares.IsAuthorized(), apicontrollers.Follow)
	apiv1.DELETE("/users/:uuid/follow", middlewares.IsAuthorized(), apicontrollers.Unfollow)
	apiv1.GET("/user/follows", middlewares.IsAuthorized(), apicontrollers.GetFollows)
	apiv1.POST("/user/follows/approval", middlewares.IsAuthorized(), apicontrollers.SetFollowApproval)
	apiv1.POST("/user/followers/:uuid/accept", middlewares.IsAuthorized(), apicontrollers.AcceptFollower)
	apiv1.DELETE("/user/followers/:uuid", middlewares.IsAuthorized(), apicontrollers.RemoveFollower)
	apiv1.GET("/feed", middlewares.IsAuthorized(), apicontrollers.GetFeed)

	// recording devices, created at the import of their first activity; their settings change how their
	// activities are analysed
	apiv1.GET("/device/all", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.GetDevices)
	apiv1.GET("/device/:id", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.GetDevice)
	apiv1.POST("/device/:id", middlewares.IsAuthorized(models.ActivitiesWrite), apicontrollers.UpdateDevice)
	// the same unit, that its files named differently: its activities, past and future, go to the other one
	apiv1.POST("/device/:id/merge", middlewares.IsAuthorized(models.ActivitiesWrite), apicontrollers.MergeDevice)
	// its activities analysed again with its altitude correction, in the background
	apiv1.POST("/device/:id/recompute", middlewares.IsAuthorized(models.ActivitiesWrite), apicontrollers.RecomputeDevice)

	// routes
	apiv1.POST("/route/:id/commute", middlewares.IsAuthorized(models.ActivitiesWrite), apicontrollers.SetRouteCommute)

	// equipments
	apiv1.POST("/equipment", middlewares.IsAuthorized(models.EquipmentsCreate), apicontrollers.CreateEquipment)
	apiv1.GET("/equipment/:id", middlewares.IsAuthorized(models.EquipmentsRead), apicontrollers.GetEquipment)
	apiv1.GET("/equipment/all", middlewares.IsAuthorized(models.EquipmentsRead), apicontrollers.GetEquipments)
	apiv1.DELETE("/equipment/:id", middlewares.IsAuthorized(models.EquipmentsDelete), apicontrollers.DeleteEquipment)
	apiv1.POST("/equipment/:id", middlewares.IsAuthorized(models.EquipmentsWrite), apicontrollers.UpdateEquipment)
	apiv1.POST("/equipment/:id/picture", middlewares.IsAuthorized(models.EquipmentsWrite), apicontrollers.UploadEquipmentPicture)
	apiv1.DELETE("/equipment/:id/picture", middlewares.IsAuthorized(models.EquipmentsWrite), apicontrollers.DeleteEquipmentPicture)
	apiv1.POST("/equipment/:id/commutes", middlewares.IsAuthorized(models.EquipmentsWrite, models.ActivitiesWrite),
		apicontrollers.AssignCommuteEquipment)
	// services of an equipment
	apiv1.GET("/equipment/:id/services", middlewares.IsAuthorized(models.EquipmentsRead), apicontrollers.GetServices)
	apiv1.POST("/equipment/:id/services", middlewares.IsAuthorized(models.EquipmentsWrite), apicontrollers.CreateService)
	apiv1.GET("/equipment/:id/services/:service", middlewares.IsAuthorized(models.EquipmentsRead), apicontrollers.GetService)
	apiv1.POST("/equipment/:id/services/:service", middlewares.IsAuthorized(models.EquipmentsWrite), apicontrollers.UpdateService)
	apiv1.DELETE("/equipment/:id/services/:service", middlewares.IsAuthorized(models.EquipmentsWrite), apicontrollers.DeleteService)
	// maintenance reminders of an equipment, and of all of them
	apiv1.GET("/equipment/:id/reminders", middlewares.IsAuthorized(models.EquipmentsRead), apicontrollers.GetReminders)
	apiv1.POST("/equipment/:id/reminders", middlewares.IsAuthorized(models.EquipmentsWrite), apicontrollers.CreateReminder)
	apiv1.POST("/equipment/:id/reminders/:reminder", middlewares.IsAuthorized(models.EquipmentsWrite), apicontrollers.UpdateReminder)
	apiv1.DELETE("/equipment/:id/reminders/:reminder", middlewares.IsAuthorized(models.EquipmentsWrite), apicontrollers.DeleteReminder)
	apiv1.POST("/equipment/:id/reminders/:reminder/done", middlewares.IsAuthorized(models.EquipmentsWrite), apicontrollers.ReminderDone)
	apiv1.GET("/user/maintenance", middlewares.IsAuthorized(models.EquipmentsRead), apicontrollers.UserMaintenance)

	// healthdatas
	apiv1.POST("/healthdatas", middlewares.IsAuthorized(models.HealthDatasCreate), apicontrollers.CreateHealthData)
	apiv1.POST("/healthdatas/import", middlewares.IsAuthorized(models.HealthDatasCreate), apicontrollers.ImportHealthDatas)
	apiv1.GET("/healthdatas", middlewares.IsAuthorized(models.HealthDatasRead), apicontrollers.GetHealthDatas)
	apiv1.GET("/healthdatas/target", middlewares.IsAuthorized(models.HealthDatasRead), apicontrollers.GetWeightTarget)
	apiv1.POST("/healthdatas/target", middlewares.IsAuthorized(models.HealthDatasWrite), apicontrollers.SetWeightTarget)
	apiv1.POST("/healthdatas/:id", middlewares.IsAuthorized(models.HealthDatasWrite), apicontrollers.UpdateHealthData)
	apiv1.DELETE("/healthdatas/:id", middlewares.IsAuthorized(models.HealthDatasDelete), apicontrollers.DeleteHealthData)

	return serve(ctx, newHTTPServer(listenAddr, httpRouter), App.Logger)
}

// ShutdownTimeout is how long the requests running at the shutdown have to finish: an upload waits for an
// analysis slot (activityService.QueueTimeout), then is analysed and drawn. Below the stop_grace_period of
// the backend in docker-compose.yaml, after which the container is killed.
var ShutdownTimeout = 90 * time.Second

// serve runs server until ctx is done, then stops accepting connections and waits for the requests running,
// at most ShutdownTimeout
func serve(ctx context.Context, server *http.Server, logger zerolog.Logger) error {
	failed := make(chan error, 1)
	go func() { failed <- server.ListenAndServe() }()
	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	logger.Info().Dur("timeout", ShutdownTimeout).Msg("shutting down: finishing the requests running")
	shutdown, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		// the requests still running are cut
		_ = server.Close()
		return fmt.Errorf("requests still running after %v: %w", ShutdownTimeout, err)
	}
	logger.Info().Msg("HTTP server stopped")
	return nil
}

// The limits of the HTTP connections: without them (gin's Run), a client sending its request slowly, or
// reading the response slowly, holds a connection and its goroutine forever
var (
	// ReadHeaderTimeout: the headers come at once from a real client, slowly from a slowloris attack
	ReadHeaderTimeout = 10 * time.Second
	// ReadTimeout reads the whole request: FitBodyLimit over a slow mobile connection
	ReadTimeout = 5 * time.Minute
	// WriteTimeout counts from the end of the headers until the response is sent, so it holds the upload
	// of the body, the wait for an analysis slot (activityService.QueueTimeout), the analysis and the
	// thumbnail
	WriteTimeout = 10 * time.Minute
	// IdleTimeout closes the kept-alive connections without request
	IdleTimeout = 2 * time.Minute
)

// MaxHeaderBytes: the headers of the API are small, the session token the largest
const MaxHeaderBytes = 64 << 10

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: ReadHeaderTimeout,
		ReadTimeout:       ReadTimeout,
		WriteTimeout:      WriteTimeout,
		IdleTimeout:       IdleTimeout,
		MaxHeaderBytes:    MaxHeaderBytes,
	}
}
