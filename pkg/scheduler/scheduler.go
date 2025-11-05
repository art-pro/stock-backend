package scheduler

import (
	"time"

	"github.com/artpro/assessapp/pkg/config"
	"github.com/artpro/assessapp/pkg/models"
	"github.com/artpro/assessapp/pkg/services"
	"github.com/go-co-op/gocron"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// InitScheduler initializes the cron scheduler for automatic updates
func InitScheduler(db *gorm.DB, cfg *config.Config, logger zerolog.Logger) {
	s := gocron.NewScheduler(time.UTC)
	apiService := services.NewExternalAPIService(cfg)

	// Daily update job
	s.Every(1).Day().At("00:00").Do(func() {
		logger.Info().Msg("Running daily stock update")
		updateStocksWithFrequency(db, apiService, logger, "daily")
	})

	// Weekly update job (Mondays)
	s.Every(1).Monday().At("00:00").Do(func() {
		logger.Info().Msg("Running weekly stock update")
		updateStocksWithFrequency(db, apiService, logger, "weekly")
	})

	// Monthly update job (1st of month)
	s.Every(1).Month(1).At("00:00").Do(func() {
		logger.Info().Msg("Running monthly stock update")
		updateStocksWithFrequency(db, apiService, logger, "monthly")
	})

	// Alert check job (every hour)
	s.Every(1).Hour().Do(func() {
		checkAndSendAlerts(db, cfg, logger)
	})

	s.StartAsync()
	logger.Info().Msg("Scheduler initialized and started")
}

// updateStocksWithFrequency updates all stocks with the specified frequency
func updateStocksWithFrequency(db *gorm.DB, apiService *services.ExternalAPIService, logger zerolog.Logger, frequency string) {
	var stocks []models.Stock
	if err := db.Where("update_frequency = ?", frequency).Find(&stocks).Error; err != nil {
		logger.Error().Err(err).Str("frequency", frequency).Msg("Failed to fetch stocks for update")
		return
	}

	logger.Info().Int("count", len(stocks)).Str("frequency", frequency).Msg("Updating stocks")

	for i := range stocks {
		if err := updateStock(db, apiService, &stocks[i], logger); err != nil {
			logger.Warn().Err(err).Str("ticker", stocks[i].Ticker).Msg("Failed to update stock")
		} else {
			logger.Debug().Str("ticker", stocks[i].Ticker).Msg("Stock updated successfully")
		}

		// Add a small delay to avoid rate limiting
		time.Sleep(1 * time.Second)
	}
}

// updateStock updates a single stock's data
func updateStock(db *gorm.DB, apiService *services.ExternalAPIService, stock *models.Stock, logger zerolog.Logger) error {
	oldEV := stock.ExpectedValue

	// Fetch current price
	price, err := apiService.FetchStockPrice(stock.Ticker)
	if err != nil {
		return err
	}
	stock.CurrentPrice = price

	// Fetch Grok calculations
	if err := apiService.FetchGrokCalculations(stock); err != nil {
		return err
	}

	// Calculate derived metrics
	services.CalculateMetrics(stock)

	// Get FX rate for USD conversion
	fxRate, err := apiService.FetchExchangeRate(stock.Currency)
	if err != nil {
		fxRate = 1.0
	}

	// Calculate USD values
	stock.CurrentValueUSD = float64(stock.SharesOwned) * stock.CurrentPrice * fxRate
	costBasis := float64(stock.SharesOwned) * stock.AvgPriceLocal * fxRate
	stock.UnrealizedPnL = stock.CurrentValueUSD - costBasis

	stock.LastUpdated = time.Now()

	// Save to database
	if err := db.Save(stock).Error; err != nil {
		return err
	}

	// Create history entry
	history := models.StockHistory{
		StockID:             stock.ID,
		Ticker:              stock.Ticker,
		CurrentPrice:        stock.CurrentPrice,
		FairValue:           stock.FairValue,
		UpsidePotential:     stock.UpsidePotential,
		DownsideRisk:        stock.DownsideRisk,
		ProbabilityPositive: stock.ProbabilityPositive,
		ExpectedValue:       stock.ExpectedValue,
		KellyFraction:       stock.KellyFraction,
		Weight:              stock.Weight,
		Assessment:          stock.Assessment,
		RecordedAt:          time.Now(),
	}
	db.Create(&history)

	// Check for alerts
	var settings models.PortfolioSettings
	db.First(&settings)

	evChange := stock.ExpectedValue - oldEV
	if settings.AlertsEnabled && (evChange > settings.AlertThresholdEV || evChange < -settings.AlertThresholdEV) {
		alert := models.Alert{
			StockID:   stock.ID,
			Ticker:    stock.Ticker,
			AlertType: "ev_change",
			Message:   "EV changed from " + formatFloat(oldEV) + "% to " + formatFloat(stock.ExpectedValue) + "%",
			EmailSent: false,
			CreatedAt: time.Now(),
		}
		db.Create(&alert)
	}

	// Check if in buy zone
	if stock.CurrentPrice >= stock.BuyZoneMin && stock.CurrentPrice <= stock.BuyZoneMax {
		alert := models.Alert{
			StockID:   stock.ID,
			Ticker:    stock.Ticker,
			AlertType: "buy_zone",
			Message:   stock.Ticker + " is in buy zone at " + formatFloat(stock.CurrentPrice),
			EmailSent: false,
			CreatedAt: time.Now(),
		}
		db.Create(&alert)
	}

	return nil
}

// checkAndSendAlerts checks for unsent alerts and sends emails
func checkAndSendAlerts(db *gorm.DB, cfg *config.Config, logger zerolog.Logger) {
	var settings models.PortfolioSettings
	db.First(&settings)

	if !settings.AlertsEnabled {
		return
	}

	var alerts []models.Alert
	if err := db.Where("email_sent = ?", false).Find(&alerts).Error; err != nil {
		logger.Error().Err(err).Msg("Failed to fetch unsent alerts")
		return
	}

	if len(alerts) == 0 {
		return
	}

	logger.Info().Int("count", len(alerts)).Msg("Found unsent alerts")

	alertService := services.NewAlertService(cfg, logger)

	for _, alert := range alerts {
		if err := alertService.SendAlert(alert); err != nil {
			logger.Warn().Err(err).Uint("alert_id", alert.ID).Msg("Failed to send alert")
		} else {
			alert.EmailSent = true
			db.Save(&alert)
			logger.Info().Uint("alert_id", alert.ID).Msg("Alert sent successfully")
		}
	}
}

func formatFloat(f float64) string {
	return string(rune(int(f*100))) + "." + string(rune(int(f*100)%100))
}

