package main

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeProductRepository struct {
	products map[uuid.UUID]Product
	page     Page
	created  Product
	updated  Product
	locked   [2]uuid.UUID

	createErr error
	getErr    error
	listErr   error
	updateErr error
}

func (r *fakeProductRepository) Create(_ context.Context, product Product) error {
	r.created = product
	if r.createErr != nil {
		return r.createErr
	}
	if r.products == nil {
		r.products = make(map[uuid.UUID]Product)
	}
	r.products[product.ID] = product
	return nil
}

func (r *fakeProductRepository) Get(_ context.Context, id uuid.UUID) (Product, error) {
	if r.getErr != nil {
		return Product{}, r.getErr
	}
	product, ok := r.products[id]
	if !ok {
		return Product{}, ErrProductNotFound
	}
	return product, nil
}

func (r *fakeProductRepository) List(_ context.Context, page Page) ([]Product, PageInfo, error) {
	r.page = page
	if r.listErr != nil {
		return nil, PageInfo{}, r.listErr
	}
	return nil, PageInfo{Page: page.Number, PageSize: page.Size, Total: 0}, nil
}

func (r *fakeProductRepository) Update(_ context.Context, product Product) error {
	r.updated = product
	if r.updateErr != nil {
		return r.updateErr
	}
	r.products[product.ID] = product
	return nil
}

func (r *fakeProductRepository) Lock(_ context.Context, productID, auctionID uuid.UUID) error {
	r.locked = [2]uuid.UUID{productID, auctionID}
	return nil
}

func (r *fakeProductRepository) Unlock(_ context.Context, productID, auctionID uuid.UUID) error {
	r.locked = [2]uuid.UUID{productID, auctionID}
	return nil
}

func TestProductServiceCreateRejectsInvalidFields(t *testing.T) {
	service := NewProductService(&fakeProductRepository{})
	sellerID := uuid.New()
	for _, test := range []struct {
		name  string
		input CreateProductInput
		want  error
	}{
		{name: "missing name", input: CreateProductInput{Description: "description", Quantity: 1}, want: ErrInvalidProduct},
		{name: "missing description", input: CreateProductInput{Name: "name", Quantity: 1}, want: ErrInvalidProduct},
		{name: "non-positive quantity", input: CreateProductInput{Name: "name", Description: "description", Quantity: 0}, want: ErrInvalidProduct},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.Create(context.Background(), sellerID, test.input)
			if !errors.Is(err, test.want) {
				t.Fatalf("error: got %v, want %v", err, test.want)
			}
		})
	}
}

func TestProductServiceCreateSetsAvailableProduct(t *testing.T) {
	repository := &fakeProductRepository{}
	service := NewProductService(repository)
	sellerID := uuid.New()

	product, err := service.Create(context.Background(), sellerID, CreateProductInput{Name: "camera", Description: "used", Quantity: 2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if product.ID == uuid.Nil || product.SellerID != sellerID || product.Status != ProductStatusAvailable {
		t.Fatalf("product: %#v", product)
	}
	if repository.created.ID != product.ID {
		t.Fatalf("repository received %#v", repository.created)
	}
}

func TestProductServiceUpdateRejectsNonOwner(t *testing.T) {
	productID := uuid.New()
	repository := &fakeProductRepository{products: map[uuid.UUID]Product{
		productID: {ID: productID, SellerID: uuid.New(), Status: ProductStatusAvailable},
	}}
	service := NewProductService(repository)

	err := service.Update(context.Background(), uuid.New(), productID, UpdateProductInput{Name: "new", Description: "new", Quantity: 1})
	if !errors.Is(err, ErrProductNotOwner) {
		t.Fatalf("error: got %v, want %v", err, ErrProductNotOwner)
	}
}

func TestProductServiceUpdateRejectsLockedProduct(t *testing.T) {
	productID, sellerID := uuid.New(), uuid.New()
	repository := &fakeProductRepository{products: map[uuid.UUID]Product{
		productID: {ID: productID, SellerID: sellerID, Status: ProductStatusLocked},
	}}
	service := NewProductService(repository)

	err := service.Update(context.Background(), sellerID, productID, UpdateProductInput{Name: "new", Description: "new", Quantity: 1})
	if !errors.Is(err, ErrProductLocked) {
		t.Fatalf("error: got %v, want %v", err, ErrProductLocked)
	}
}

func TestProductServiceListBoundsPageSize(t *testing.T) {
	repository := &fakeProductRepository{}
	service := NewProductService(repository)

	_, pageInfo, err := service.List(context.Background(), Page{Number: 0, Size: 1000})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if repository.page.Number != 1 || repository.page.Size != MaxPageSize || pageInfo.PageSize != MaxPageSize {
		t.Fatalf("page: repository=%#v info=%#v", repository.page, pageInfo)
	}
}

func TestProductServiceLockUnlockAreIdempotentByAuction(t *testing.T) {
	productID, auctionID := uuid.New(), uuid.New()
	repository := &fakeProductRepository{products: map[uuid.UUID]Product{
		productID: {ID: productID, SellerID: uuid.New(), Status: ProductStatusAvailable},
	}}
	service := NewProductService(repository)

	if err := service.Lock(context.Background(), productID, auctionID); err != nil {
		t.Fatalf("first lock: %v", err)
	}
	if err := service.Lock(context.Background(), productID, auctionID); err != nil {
		t.Fatalf("second lock: %v", err)
	}
	if err := service.Unlock(context.Background(), productID, auctionID); err != nil {
		t.Fatalf("first unlock: %v", err)
	}
	if err := service.Unlock(context.Background(), productID, auctionID); err != nil {
		t.Fatalf("second unlock: %v", err)
	}
}
