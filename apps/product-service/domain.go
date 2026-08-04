package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	ProductStatusDraft     = "draft"
	ProductStatusAvailable = "available"
	ProductStatusLocked    = "locked"
	DefaultPageSize        = 20
	MaxPageSize            = 100
)

var (
	ErrProductNotFound = errors.New("product not found")
	ErrProductNotOwner = errors.New("product seller does not own product")
	ErrProductLocked   = errors.New("product is locked")
	ErrInvalidProduct  = errors.New("invalid product")
	ErrInvalidPage     = errors.New("invalid page")
	ErrUnauthorized    = errors.New("unauthorized")
	ErrForbidden       = errors.New("forbidden")
)

type Product struct {
	ID          uuid.UUID  `json:"id"`
	SellerID    uuid.UUID  `json:"seller_id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Quantity    int        `json:"quantity"`
	Status      string     `json:"status"`
	AuctionID   *uuid.UUID `json:"auction_id,omitempty"`
	Version     int64      `json:"version"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CreateProductInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Quantity    int    `json:"quantity"`
}

type UpdateProductInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Quantity    int    `json:"quantity"`
}

type Page struct {
	Number int `json:"page"`
	Size   int `json:"page_size"`
}

type PageInfo struct {
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
}

type ProductRepository interface {
	Create(context.Context, Product) error
	Get(context.Context, uuid.UUID) (Product, error)
	List(context.Context, Page) ([]Product, PageInfo, error)
	Update(context.Context, Product) error
	Lock(context.Context, uuid.UUID, uuid.UUID) error
	Unlock(context.Context, uuid.UUID, uuid.UUID) error
}

type ProductService interface {
	Create(context.Context, uuid.UUID, CreateProductInput) (Product, error)
	Get(context.Context, uuid.UUID) (Product, error)
	List(context.Context, Page) ([]Product, PageInfo, error)
	Update(context.Context, uuid.UUID, uuid.UUID, UpdateProductInput) error
}

type internalProductService interface {
	Ownership(context.Context, uuid.UUID, uuid.UUID) (Product, error)
	Lock(context.Context, uuid.UUID, uuid.UUID) error
	Unlock(context.Context, uuid.UUID, uuid.UUID) error
}

type productService struct{ repository ProductRepository }

func NewProductService(repository ProductRepository) *productService {
	return &productService{repository: repository}
}

func (s *productService) Create(ctx context.Context, sellerID uuid.UUID, input CreateProductInput) (Product, error) {
	if sellerID == uuid.Nil || !validProductInput(input.Name, input.Description, input.Quantity) {
		return Product{}, ErrInvalidProduct
	}
	now := time.Now().UTC()
	product := Product{
		ID: uuid.New(), SellerID: sellerID, Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description),
		Quantity: input.Quantity, Status: ProductStatusAvailable, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repository.Create(ctx, product); err != nil {
		return Product{}, err
	}
	return product, nil
}

func (s *productService) Get(ctx context.Context, id uuid.UUID) (Product, error) {
	if id == uuid.Nil {
		return Product{}, ErrProductNotFound
	}
	return s.repository.Get(ctx, id)
}

func (s *productService) List(ctx context.Context, page Page) ([]Product, PageInfo, error) {
	page = normalizePage(page)
	return s.repository.List(ctx, page)
}

func (s *productService) Update(ctx context.Context, sellerID, productID uuid.UUID, input UpdateProductInput) error {
	if sellerID == uuid.Nil || productID == uuid.Nil || !validProductInput(input.Name, input.Description, input.Quantity) {
		return ErrInvalidProduct
	}
	product, err := s.repository.Get(ctx, productID)
	if err != nil {
		return err
	}
	if product.SellerID != sellerID {
		return ErrProductNotOwner
	}
	if product.Status == ProductStatusLocked {
		return ErrProductLocked
	}
	product.Name = strings.TrimSpace(input.Name)
	product.Description = strings.TrimSpace(input.Description)
	product.Quantity = input.Quantity
	product.UpdatedAt = time.Now().UTC()
	return s.repository.Update(ctx, product)
}

func (s *productService) Ownership(ctx context.Context, productID, userID uuid.UUID) (Product, error) {
	product, err := s.Get(ctx, productID)
	if err != nil {
		return Product{}, err
	}
	if userID == uuid.Nil || product.SellerID != userID {
		return Product{}, ErrProductNotOwner
	}
	return product, nil
}

func (s *productService) Lock(ctx context.Context, productID, auctionID uuid.UUID) error {
	if productID == uuid.Nil || auctionID == uuid.Nil {
		return ErrInvalidProduct
	}
	return s.repository.Lock(ctx, productID, auctionID)
}

func (s *productService) Unlock(ctx context.Context, productID, auctionID uuid.UUID) error {
	if productID == uuid.Nil || auctionID == uuid.Nil {
		return ErrInvalidProduct
	}
	return s.repository.Unlock(ctx, productID, auctionID)
}

func validProductInput(name, description string, quantity int) bool {
	return strings.TrimSpace(name) != "" && strings.TrimSpace(description) != "" && quantity > 0
}

func normalizePage(page Page) Page {
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < 1 {
		page.Size = DefaultPageSize
	}
	if page.Size > MaxPageSize {
		page.Size = MaxPageSize
	}
	return page
}
