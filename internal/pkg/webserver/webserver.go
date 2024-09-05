package webserver

import (
	"MySportWeb/internal/pkg/apicontrollers"
	"MySportWeb/internal/pkg/app"
	"MySportWeb/internal/pkg/controllers"
	"MySportWeb/internal/pkg/middlewares"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/contrib/static"
	"github.com/gin-gonic/gin"
	"net/http"
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
	config := cors.DefaultConfig()
	// config.AllowAllOrigins = true
	config.AllowOrigins = []string{"http://localhost:3000"}
	config.AddAllowMethods("OPTIONS")
	config.AllowHeaders = []string{"Authorization", "Content-Type"}
	httpRouter.Use(cors.New(config))

	httpRouter.LoadHTMLGlob("templates/*")
	httpRouter.Use(static.Serve("/static", static.LocalFile("./static", true)))

	httpRouter.Use(AppHandler(App))
	httpRouter.GET("/", func(c *gin.Context) {
		c.HTML(http.StatusOK, "index.tmpl", gin.H{
			"title":    "MySportWeb",
			"vapidkey": App.Notifications.PubKey,
		})
	})
	httpRouter.GET("/login", controllers.Login)
	httpRouter.GET("/signup", controllers.SignUp)

	httpRouter.GET("/mewactivity", middlewares.IsAuthorized())

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

	// Activities
	apiv1.POST("/activity", middlewares.IsAuthorized(), apicontrollers.UploadActivity)
	apiv1.GET("/activity/list", middlewares.IsAuthorized(), apicontrollers.ListActivities)
	apiv1.GET("/activity/:id", middlewares.IsAuthorized(), apicontrollers.GetActivity)
	apiv1.DELETE("/activity/:id", middlewares.IsAuthorized(), apicontrollers.DeleteActivity)
	apiv1.POST("/activity/:id", middlewares.IsAuthorized(), apicontrollers.UpdateActivity)

	// equipments
	apiv1.POST("/equipment", middlewares.IsAuthorized(), apicontrollers.CreateEquipment)
	apiv1.GET("/equipment/:id", middlewares.IsAuthorized(), apicontrollers.GetEquipment)
	apiv1.GET("/equipment/all", middlewares.IsAuthorized(), apicontrollers.GetEquipments)
	apiv1.DELETE("/equipment/:id", middlewares.IsAuthorized(), apicontrollers.DeleteEquipment)
	apiv1.POST("/equipment/:id", middlewares.IsAuthorized(), apicontrollers.UpdateEquipment)

	// Start and run the server
	err := httpRouter.Run(listenAddr)
	return err

}
