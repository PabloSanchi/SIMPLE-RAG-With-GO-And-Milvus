package datastore

import (
	"github.com/pablosanchi/datastore/core/domain"
	"github.com/pablosanchi/datastore/core/ports"
	"github.com/pablosanchi/datastore/core/ports/secondary"
	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"
	"fmt"
	"context"
)

type DatastoreMilvusRepository struct{
	Client client.Client
	Encoder secondary.TextEncoder
}

func NewDatastoreMilvusRepository(milvusClient client.Client, encoder secondary.TextEncoder) ports.DatastoreRepository {
	return &DatastoreMilvusRepository{
		Client: milvusClient, 
		Encoder: encoder,
	}
}

func (m *DatastoreMilvusRepository) CreateCollection(collectionName string) error {
    schema := defineSchema(collectionName)
    if err := m.Client.CreateCollection(context.Background(), schema, entity.DefaultShardNumber); err != nil {
        return fmt.Errorf("failed to create collection: %w", err)
    }

    if err := m.buildIndex(collectionName); err != nil {
        return fmt.Errorf("failed to build index: %w", err)
    }

    return nil
}

func (m *DatastoreMilvusRepository) DeleteCollection(collectionName string) error {
    if err := m.Client.DropCollection(context.Background(), collectionName); err != nil {
        return fmt.Errorf("failed to drop collection: %w", err)
    }

    return nil
}

func (m *DatastoreMilvusRepository) List() ([]string, error) {
    listColl, err := m.Client.ListCollections(context.Background())
    if err != nil {
        return nil, fmt.Errorf("failed to list collections: %w", err)
    }

    var collections []string
    for _, collection := range listColl {
        collections = append(collections, collection.Name)
    }

    return collections, nil
}

func (m *DatastoreMilvusRepository) UpsertDocuments(collectionName string, documents []domain.Document) error {
	nEntities := len(documents)
	idList:= make([]string, 0, nEntities)
	titleList:= make([]string, 0, nEntities)
	contentList:= make([]string, 0, nEntities)
	categoryList:= make([]string, 0, nEntities)
	embeddingList := make([][]float32, 0, nEntities)

	for _, document := range documents {
		encodedContent, err := m.Encoder.Encode(document.Content)

		if err != nil {
			return fmt.Errorf("fail to encode content: %w", err)
		}
		
		idList = append(idList, document.ID)
		titleList = append(titleList, document.Title)
		contentList = append(contentList, document.Content)
		categoryList = append(categoryList, document.Category)
		embeddingList = append(embeddingList, encodedContent)
	}

	idColumn := entity.NewColumnVarChar("id", idList)
	titleColumn := entity.NewColumnVarChar("title", titleList)
	contentColumn := entity.NewColumnVarChar("content", contentList)
	categoryColumn := entity.NewColumnVarChar("category", categoryList)
	embeddingColumn := entity.NewColumnFloatVector("embedding", 4096, embeddingList)

	if _, err := m.Client.Upsert(
		context.Background(), 
		collectionName, 
		"",
		idColumn,
		titleColumn,
		contentColumn,
		categoryColumn,
		embeddingColumn,	
	);

	err != nil {
		return fmt.Errorf("fail to upsert data, err: %w", err)
	}

	return nil
}

func (m *DatastoreMilvusRepository) Search(collectionName string, query string) ([]domain.Document, error) {

	encodedQuery, err := m.Encoder.Encode(query)

	if err != nil {
		return nil, fmt.Errorf("fail to encode query: %w", err)
	}

	if err := m.Client.LoadCollection(context.Background(), collectionName, false, ); err != nil {
		return nil, fmt.Errorf("failed to load collection: %w", err)
	}

	sp, _ := entity.NewIndexIvfFlatSearchParam(10,)
	
	opt := client.SearchQueryOptionFunc(func(option *client.SearchQueryOption) {
		option.Limit = 3
		option.Offset = 0
		option.ConsistencyLevel = entity.ClStrong
		option.IgnoreGrowing = false
	})

	searchResult, err := m.Client.Search(
		context.Background(),
		collectionName,
		[]string{},
		"",
		[]string{"title", "content", "category"},
		[]entity.Vector{entity.FloatVector(encodedQuery)},
		"embedding",
		entity.COSINE,
		10,
		sp,
		opt,
	)

	if err != nil {
		return nil, fmt.Errorf("fail to search collection: %w", err)
	}

	fields := searchResult[0].Fields
	titleList := fields.GetColumn("title")
	contentList := fields.GetColumn("content")
	categoryList := fields.GetColumn("category")

	var documents []domain.Document
	for i := 0; i < titleList.Len(); i++ {
		
		title, _ := titleList.GetAsString(i);
		content, _ := contentList.GetAsString(i);
		category, _ := categoryList.GetAsString(i);

		documents = append(documents, domain.Document{
			ID: "",
			Title: title,
			Content: content,
			Category: category,
		})
	}
	
	err = m.Client.ReleaseCollection(
		context.Background(),                            // ctx
		collectionName,                                   // CollectionName
	)

	if err != nil {
		return nil, fmt.Errorf("failed to release collection: %w", err)
	}

	return documents, nil
}

func (m *DatastoreMilvusRepository) buildIndex(collectionName string) error {
    idx, err := entity.NewIndexIvfFlat(entity.COSINE, 1024)
    if err != nil {
        return fmt.Errorf("fail to create IVF flat index parameter: %w", err)
    }

    err = m.Client.CreateIndex(context.Background(), collectionName, "embedding", idx, false)
    if err != nil {
        return fmt.Errorf("fail to create index: %w", err)
    }

    return nil
}

func defineSchema(collectionName string) *entity.Schema {
	return &entity.Schema{
		CollectionName: collectionName,
		Description:    "",
		AutoID:         false,
		Fields: []*entity.Field{
			{
				Name:       "id",
				DataType:   entity.FieldTypeVarChar,
				PrimaryKey: true,
				AutoID:     false,
				TypeParams: map[string]string{
					entity.TypeParamMaxLength: fmt.Sprintf("%d", 255),
				},
			},
			{
				Name:       "title",
				DataType:   entity.FieldTypeVarChar,
				PrimaryKey: false,
				AutoID:     false,
				TypeParams: map[string]string{
					entity.TypeParamMaxLength: fmt.Sprintf("%d", 255),
				},
			},
			{
				Name:       "content",
				DataType:   entity.FieldTypeVarChar,
				PrimaryKey: false,
				AutoID:     false,
				TypeParams: map[string]string{
					entity.TypeParamMaxLength: fmt.Sprintf("%d", 3000),
				},
			},
			{
				Name:       "category",
				DataType:   entity.FieldTypeVarChar,
				PrimaryKey: false,
				AutoID:     false,
				TypeParams: map[string]string{
					entity.TypeParamMaxLength: fmt.Sprintf("%d", 100),
				},
			},
			{
				Name:     "embedding",
				DataType: entity.FieldTypeFloatVector,
				TypeParams: map[string]string{
					entity.TypeParamDim: fmt.Sprintf("%d", 4096),
				},
			},
		},
	}
}
