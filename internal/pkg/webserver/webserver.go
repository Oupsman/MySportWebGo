package webserver

import (
	"MySportWeb/internal/pkg/apicontrollers"
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/controllers"
	"MySportWeb/internal/pkg/middlewares"
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
	apiv1.GET("/user/logout", middlewares.IsAuthorized(), apicontrollers.Logout)
	apiv1.GET("/user/dashboard", middlewares.IsAuthorized(), apicontrollers.Dashboard)
	apiv1.GET("/user/calendar", middlewares.IsAuthorized(), apicontrollers.Calendar)
	apiv1.GET("/user", middlewares.IsAuthorized(), apicontrollers.GetUser)
	apiv1.POST("/user", middlewares.IsAuthorized(), apicontrollers.UpdateUser)

	// Activities
	apiv1.POST("/activity", middlewares.IsAuthorized(), apicontrollers.UploadActivity)
	apiv1.GET("/activity/list/:start/:count", middlewares.IsAuthorized(), apicontrollers.ListActivities)
	apiv1.GET("/activity/:id", apicontrollers.GetActivity)
	apiv1.GET("/activity/:id/similar", middlewares.IsAuthorized(), apicontrollers.GetSimilarActivities)
	apiv1.DELETE("/activity/:id", middlewares.IsAuthorized(), apicontrollers.DeleteActivity)
	apiv1.POST("/activity/:id", middlewares.IsAuthorized(), apicontrollers.UpdateActivity)

	// equipments
	apiv1.POST("/equipment", middlewares.IsAuthorized(), apicontrollers.CreateEquipment)
	apiv1.GET("/equipment/:id", middlewares.IsAuthorized(), apicontrollers.GetEquipment)
	apiv1.GET("/equipment/all", middlewares.IsAuthorized(), apicontrollers.GetEquipments)
	apiv1.DELETE("/equipment/:id", middlewares.IsAuthorized(), apicontrollers.DeleteEquipment)
	apiv1.POST("/equipment/:id", middlewares.IsAuthorized(), apicontrollers.UpdateEquipment)

	// healthdatas
	apiv1.POST("/healthdatas", middlewares.IsAuthorized(), apicontrollers.CreateHealthData)
	apiv1.POST("/healthdatas/import", middlewares.IsAuthorized(), apicontrollers.ImportHealthDatas)
	apiv1.GET("/healthdatas", middlewares.IsAuthorized(), apicontrollers.GetHealthDatas)

	// Start and run the server
	err := httpRouter.Run(listenAddr)
	return err

}
