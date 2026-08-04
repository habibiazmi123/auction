package main

import (
	"context"
	"errors"
	"math"
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
	product, ok := r.products[productID]
	if !ok {
		return ErrProductNotFound
	}
	updated, changed, err := transitionProductLock(product, auctionID, true)
	if err != nil {
		return err
	}
	if changed {
		r.products[productID] = updated
	}
	r.locked = [2]uuid.UUID{productID, auctionID}
	return nil
}

func (r *fakeProductRepository) Unlock(_ context.Context, productID, auctionID uuid.UUID) error {
	product, ok := r.products[productID]
	if !ok {
		return ErrProductNotFound
	}
	updated, changed, err := transitionProductLock(product, auctionID, false)
	if err != nil {
		return err
	}
	if changed {
		r.products[productID] = updated
	}
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

func TestProductServiceListRejectsInvalidPage(t *testing.T) {
	repository := &fakeProductRepository{}
	service := NewProductService(repository)

	for _, page := range []Page{
		{Number: 0, Size: DefaultPageSize},
		{Number: 1, Size: MaxPageSize + 1},
		{Number: -1, Size: 1},
		{Number: math.MaxInt, Size: MaxPageSize},
	} {
		if _, _, err := service.List(context.Background(), page); !errors.Is(err, ErrInvalidPage) {
			t.Fatalf("page=%#v: error=%v, want %v", page, err, ErrInvalidPage)
		}
	}
}

func TestTransitionProductLockStateMatrix(t *testing.T) {
	auctionID, otherAuctionID := uuid.New(), uuid.New()
	tests := []struct {
		name       string
		product    Product
		lock       bool
		auctionID  uuid.UUID
		wantStatus string
		wantChange bool
		wantErr    error
	}{
		{name: "available locks", product: Product{Status: ProductStatusAvailable}, lock: true, auctionID: auctionID, wantStatus: ProductStatusLocked, wantChange: true},
		{name: "draft cannot lock", product: Product{Status: ProductStatusDraft}, lock: true, auctionID: auctionID, wantErr: ErrProductUnavailable},
		{name: "same lock is idempotent", product: Product{Status: ProductStatusLocked, AuctionID: &auctionID}, lock: true, auctionID: auctionID, wantStatus: ProductStatusLocked},
		{name: "different lock is rejected", product: Product{Status: ProductStatusLocked, AuctionID: &auctionID}, lock: true, auctionID: otherAuctionID, wantErr: ErrProductLocked},
		{name: "available cannot unlock", product: Product{Status: ProductStatusAvailable}, auctionID: auctionID, wantErr: ErrProductLocked},
		{name: "locked matching auction unlocks", product: Product{Status: ProductStatusLocked, AuctionID: &auctionID}, auctionID: auctionID, wantStatus: ProductStatusAvailable, wantChange: true},
		{name: "locked different auction cannot unlock", product: Product{Status: ProductStatusLocked, AuctionID: &auctionID}, auctionID: otherAuctionID, wantErr: ErrProductLocked},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, changed, err := transitionProductLock(test.product, test.auctionID, test.lock)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error: got %v, want %v", err, test.wantErr)
			}
			if test.wantErr != nil {
				return
			}
			if got.Status != test.wantStatus || changed != test.wantChange {
				t.Fatalf("transition: got=%#v changed=%v", got, changed)
			}
			if test.lock && got.AuctionID == nil {
				t.Fatal("lock transition lost auction ID")
			}
			if !test.lock && got.AuctionID != nil {
				t.Fatal("unlock transition retained auction ID")
			}
		})
	}
}

func TestProductServiceLockUnlockEnforcesAuctionState(t *testing.T) {
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
	if err := service.Unlock(context.Background(), productID, auctionID); !errors.Is(err, ErrProductLocked) {
		t.Fatalf("second unlock error: got %v, want %v", err, ErrProductLocked)
	}
}
