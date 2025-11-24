package handler_test

import (
	"github.com/JimAlex927/tusd-backend/pkg/filestore"
	"github.com/JimAlex927/tusd-backend/pkg/handler"
	"github.com/JimAlex927/tusd-backend/pkg/memorylocker"
)

func ExampleNewStoreComposer() {
	composer := handler.NewStoreComposer()

	fs := filestore.New("./data")
	fs.UseIn(composer)

	ml := memorylocker.New()
	ml.UseIn(composer)

	config := handler.Config{
		StoreComposer: composer,
	}

	_, _ = handler.NewHandler(config)
}
