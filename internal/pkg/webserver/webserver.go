package webserver

import (
	"MySportWeb/internal/pkg/apicontrollers"
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/controllers"
	"MySportWeb/internal/pkg/middlewares"
	"MySportWeb/internal/pkg/models"
	"MySportWeb/internal/pkg/vars"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/contrib/static"
	"github.com/gin-gonic/gin"
)

type WebServer struct {
	Router *gin.Engine
}

func AppHandler(App *app.App) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("App", App)
		c.Next()
	}
}

func RunHttp(listenAddr string, App *app.App) error {

	httpRouter := gin.Default()
	httpRouter.Use(static.Serve("/MEDIA", static.LocalFile("./MEDIA", true)))
	httpRouter.Use(AppHandler(App))

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
		AllowOrigins:     []string{vars.Domain},
		AllowMethods:     []string{"GET", "OPTIONS", "POST", "DELETE"},
		AllowHeaders:     []string{"Origin", "Authorization", "Content-Type"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	httpRouter.GET("/", apicontrollers.HealthCheck)
	httpRouter.GET("/login", controllers.Login)
	httpRouter.GET("/signup", controllers.SignUp)

	apiv1 := httpRouter.Group("/api/v1")
	{
		apiv1.GET("/", middlewares.IsAuthorized(), controllers.IsAuthenticated)
	}

	apiv1.GET("/healthcheck", apicontrollers.HealthCheck)
	// Users
	apiv1.POST("/user/login", apicontrollers.Login)
	apiv1.POST("/user/register", apicontrollers.Register)
	// Routes given permissions are also open to device tokens holding them, the others to sessions only
	apiv1.GET("/user/logout", middlewares.IsAuthorized(), apicontrollers.Logout)
	apiv1.GET("/user/dashboard", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.Dashboard)
	apiv1.GET("/user/calendar", middlewares.IsAuthorized(models.ActivitiesRead), apicontrollers.Calendar)
	apiv1.GET("/user", middlewares.IsAuthorized(), apicontrollers.GetUser)
	apiv1.POST("/user", middlewares.IsAuthorized(), apicontrollers.UpdateUser)

	// device tokens, managed from a session only: a device token can't create nor extend tokens
	apiv1.GET("/user/tokens", middlewares.IsAuthorized(), apicontrollers.GetDeviceTokens)
	apiv1.POST("/user/tokens", middlewares.IsAuthorized(), apicontrollers.CreateDeviceToken)
	apiv1.DELETE("/user/tokens/:id", middlewares.IsAuthorized(), apicontrollers.DeleteDeviceToken)

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

	// healthdatas
	apiv1.POST("/healthdatas", middlewares.IsAuthorized(models.HealthDatasCreate), apicontrollers.CreateHealthData)
	apiv1.POST("/healthdatas/import", middlewares.IsAuthorized(models.HealthDatasCreate), apicontrollers.ImportHealthDatas)
	apiv1.GET("/healthdatas", middlewares.IsAuthorized(models.HealthDatasRead), apicontrollers.GetHealthDatas)
	apiv1.POST("/healthdatas/:id", middlewares.IsAuthorized(models.HealthDatasWrite), apicontrollers.UpdateHealthData)
	apiv1.DELETE("/healthdatas/:id", middlewares.IsAuthorized(models.HealthDatasDelete), apicontrollers.DeleteHealthData)

	// Start and run the server
	err := httpRouter.Run(listenAddr)
	return err

}
