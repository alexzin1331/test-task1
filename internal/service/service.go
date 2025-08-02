package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"test-task1/internal/storage"
	"test-task1/models"
)

type CurrencyHandler struct {
	storage *storage.Storage
}

func NewCurrencyHandler(storage *storage.Storage) *CurrencyHandler {
	return &CurrencyHandler{storage: storage}
}

// AddCurrency godoc
// @Summary Add cryptocurrency to tracking
// @Description Starts collecting prices for specified cryptocurrency with 15 seconds interval
// @Tags currency
// @Accept json
// @Produce json
// @Param input body models.AddCurrencyRequest true "Currency data"
// @Success 200
// @Failure 400 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /currency/add [post]
func (h *CurrencyHandler) AddCurrency(c *gin.Context) {
	var req models.AddCurrencyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid request"})
		return
	}

	h.storage.AddCurrency(req.Coin)
	c.Status(http.StatusOK)
}

// RemoveCurrency godoc
// @Summary Remove cryptocurrency from tracking
// @Description Stops collecting prices for specified cryptocurrency
// @Tags currency
// @Accept json
// @Produce json
// @Param input body models.RemoveCurrencyRequest true "Currency data"
// @Success 200
// @Failure 400 {object} models.ErrorResponse
// @Failure 500 {object} models.Error
func (h *CurrencyHandler) RemoveCurrency(c *gin.Context) {
	var req models.RemoveCurrencyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid request"})
		return
	}

	h.storage.RemoveCurrency(req.Coin)
	c.Status(http.StatusOK)
}

// GetPrice godoc
// @Summary Get cryptocurrency price
// @Description Returns cryptocurrency price at specified time or nearest available
// @Tags currency
// @Accept json
// @Produce json
// @Param input body models.PriceRequest true "Request parameters"
// @Success 200 {object} models.PriceResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /currency/price [post]
func (h *CurrencyHandler) GetPrice(c *gin.Context) {
	var req models.PriceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.ErrorResponse{Error: "invalid request"})
		return
	}

	// Если timestamp не указан, используем текущее время
	timestamp := time.Now().Unix()
	if req.Timestamp != nil {
		timestamp = *req.Timestamp
	}

	price, err := h.storage.GetPrice(req.Coin, timestamp)
	if err != nil {
		c.JSON(http.StatusNotFound, models.ErrorResponse{Error: "price not found"})
		return
	}

	response := models.PriceResponse{
		Coin:      req.Coin,
		Price:     price,
		Timestamp: timestamp,
	}

	c.JSON(http.StatusOK, response)
}
