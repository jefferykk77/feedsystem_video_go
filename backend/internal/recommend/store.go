package recommend

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"feedsystem_video_go/internal/config"
	"feedsystem_video_go/internal/video"

	milvusindexer "github.com/cloudwego/eino-ext/components/indexer/milvus2"
	milvusretriever "github.com/cloudwego/eino-ext/components/retriever/milvus2"
	"github.com/cloudwego/eino-ext/components/retriever/milvus2/search_mode"
	"github.com/cloudwego/eino/schema"
	"github.com/milvus-io/milvus/client/v2/column"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
	"gorm.io/gorm"
)

const (
	collectionName = "video_tags"
	vectorField    = "tag_vector"
	searchTopK     = 50
	embedBatch     = 16
)

// Store 用 Eino 把标签嵌入 Milvus，再按标签向量检索视频。
// Milvus 必须有主键，主键 id 由视频 ID 和标签名拼成。业务字段只有 tag_name、video_id、tag_vector。
type Store struct {
	mu        sync.RWMutex
	indexer   *milvusindexer.Indexer
	retriever *milvusretriever.Retriever
}

// Start 在后台连接 Milvus。连接成功前，检索返回空结果，推荐流用热榜和关注流补位。
func Start(cfg config.Config, db *gorm.DB) *Store {
	s := &Store{}
	if strings.TrimSpace(cfg.Embed.URL) == "" {
		log.Printf("tag vector index disabled: embed url is empty")
		return s
	}
	go func() {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			err := s.connect(ctx, cfg)
			cancel()
			if err == nil {
				log.Printf("tag vector index enabled, model=BAAI/bge-small-zh-v1.5")
				if tagErr := video.RewriteStoredTags(db); tagErr != nil {
					log.Printf("rewrite chinese tags: %v", tagErr)
				}
				syncCtx, syncCancel := context.WithTimeout(context.Background(), 5*time.Minute)
				if syncErr := s.SyncAll(syncCtx, db); syncErr != nil {
					log.Printf("sync video tags: %v", syncErr)
				}
				syncCancel()
				return
			}
			log.Printf("tag vector index waiting: %v", err)
			time.Sleep(5 * time.Second)
		}
	}()
	return s
}

func (s *Store) connect(ctx context.Context, cfg config.Config) error {
	if strings.TrimSpace(cfg.Embed.URL) == "" {
		return fmt.Errorf("embed url is empty")
	}
	if strings.TrimSpace(cfg.Milvus.Address) == "" {
		return fmt.Errorf("milvus address is empty")
	}
	embedder := newHTTPEmbedder(cfg.Embed.URL)

	milvusCfg := &milvusclient.ClientConfig{Address: cfg.Milvus.Address}
	cli, err := milvusclient.New(ctx, milvusCfg)
	if err != nil {
		return fmt.Errorf("milvus client: %w", err)
	}
	if err := ensureCollection(ctx, cli, embedDimension); err != nil {
		return err
	}

	indexer, err := milvusindexer.NewIndexer(ctx, &milvusindexer.IndexerConfig{
		Client:     cli,
		Collection: collectionName,
		Vector: &milvusindexer.VectorConfig{
			Dimension:    embedDimension,
			MetricType:   milvusindexer.COSINE,
			VectorField:  vectorField,
			IndexBuilder: milvusindexer.NewHNSWIndexBuilder().WithM(16).WithEfConstruction(200),
		},
		Embedding:         embedder,
		DocumentConverter: tagColumns,
	})
	if err != nil {
		return fmt.Errorf("milvus indexer: %w", err)
	}
	retriever, err := milvusretriever.NewRetriever(ctx, &milvusretriever.RetrieverConfig{
		Client:       cli,
		Collection:   collectionName,
		VectorField:  vectorField,
		OutputFields: []string{"id", "tag_name", "video_id"},
		TopK:         searchTopK,
		SearchMode:   search_mode.NewApproximate(milvusretriever.COSINE),
		Embedding:    embedder,
	})
	if err != nil {
		return fmt.Errorf("milvus retriever: %w", err)
	}
	s.mu.Lock()
	s.indexer = indexer
	s.retriever = retriever
	s.mu.Unlock()
	return nil
}

func ensureCollection(ctx context.Context, cli *milvusclient.Client, dim int64) error {
	has, err := cli.HasCollection(ctx, milvusclient.NewHasCollectionOption(collectionName))
	if err != nil {
		return fmt.Errorf("check milvus collection: %w", err)
	}
	if has {
		if err := cli.DropCollection(ctx, milvusclient.NewDropCollectionOption(collectionName)); err != nil {
			return fmt.Errorf("drop milvus collection: %w", err)
		}
	}
	schema := entity.NewSchema().
		WithName(collectionName).
		WithField(entity.NewField().WithName("id").WithDataType(entity.FieldTypeVarChar).WithMaxLength(255).WithIsPrimaryKey(true)).
		WithField(entity.NewField().WithName("tag_name").WithDataType(entity.FieldTypeVarChar).WithMaxLength(100)).
		WithField(entity.NewField().WithName("video_id").WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(vectorField).WithDataType(entity.FieldTypeFloatVector).WithDim(dim))
	if err := cli.CreateCollection(ctx, milvusclient.NewCreateCollectionOption(collectionName, schema)); err != nil {
		return fmt.Errorf("create milvus collection: %w", err)
	}
	return nil
}

func tagColumns(ctx context.Context, docs []*schema.Document, vectors [][]float64) ([]column.Column, error) {
	ids := make([]string, 0, len(docs))
	names := make([]string, 0, len(docs))
	videoIDs := make([]int64, 0, len(docs))
	vecs := make([][]float32, 0, len(docs))
	for i, doc := range docs {
		ids = append(ids, doc.ID)
		names = append(names, doc.Content)
		videoIDs = append(videoIDs, int64(metaUint(doc.MetaData, "video_id")))
		source := vectors[i]
		vec := make([]float32, len(source))
		for j, v := range source {
			vec[j] = float32(v)
		}
		vecs = append(vecs, vec)
	}
	dim := 0
	if len(vecs) > 0 {
		dim = len(vecs[0])
	}
	return []column.Column{
		column.NewColumnVarChar("id", ids),
		column.NewColumnVarChar("tag_name", names),
		column.NewColumnInt64("video_id", videoIDs),
		column.NewColumnFloatVector(vectorField, dim, vecs),
	}, nil
}

func (s *Store) IndexVideoTags(ctx context.Context, videoID uint, tags []string) error {
	if s == nil || videoID == 0 || len(tags) == 0 {
		return nil
	}
	s.mu.RLock()
	indexer := s.indexer
	s.mu.RUnlock()
	if indexer == nil {
		return fmt.Errorf("tag vector index is not ready")
	}
	docs := make([]*schema.Document, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		docs = append(docs, &schema.Document{
			ID:      fmt.Sprintf("%d:%s", videoID, tag),
			Content: tag,
			MetaData: map[string]any{
				"video_id": videoID,
				"tag_name": tag,
			},
		})
	}
	for start := 0; start < len(docs); start += embedBatch {
		end := start + embedBatch
		if end > len(docs) {
			end = len(docs)
		}
		var err error
		for attempt := 0; attempt < 6; attempt++ {
			_, err = indexer.Store(ctx, docs[start:end])
			if err == nil {
				break
			}
			msg := err.Error()
			if !strings.Contains(msg, "429") && !strings.Contains(msg, "RESOURCE_EXHAUSTED") {
				return err
			}
			time.Sleep(time.Duration(8*(attempt+1)) * time.Second)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SyncAll(ctx context.Context, db *gorm.DB) error {
	if s == nil || db == nil {
		return nil
	}
	type row struct {
		VideoID uint
		Name    string
	}
	var rows []row
	err := db.WithContext(ctx).
		Table("video_tags").
		Select("video_tags.video_id, tags.name").
		Joins("JOIN tags ON tags.id = video_tags.tag_id").
		Scan(&rows).Error
	if err != nil {
		return err
	}
	byVideo := map[uint][]string{}
	for _, row := range rows {
		byVideo[row.VideoID] = append(byVideo[row.VideoID], row.Name)
	}
	for videoID, tags := range byVideo {
		if err := s.IndexVideoTags(ctx, videoID, tags); err != nil {
			log.Printf("index video %d: %v", videoID, err)
		}
	}
	return nil
}

func (s *Store) SearchVideos(ctx context.Context, tags []string, limit int) ([]uint, error) {
	if s == nil || len(tags) == 0 {
		return nil, nil
	}
	s.mu.RLock()
	retriever := s.retriever
	s.mu.RUnlock()
	if retriever == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	best := map[uint]float64{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		docs, err := retriever.Retrieve(ctx, tag)
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			videoID := metaUint(doc.MetaData, "video_id")
			if videoID == 0 {
				videoID = videoIDFromDocID(doc.ID)
			}
			if videoID == 0 {
				continue
			}
			score := doc.Score()
			if old, ok := best[videoID]; !ok || score > old {
				best[videoID] = score
			}
		}
	}
	ids := make([]uint, 0, len(best))
	for id := range best {
		ids = append(ids, id)
	}
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && best[ids[j]] > best[ids[j-1]]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids, nil
}

func videoIDFromDocID(id string) uint {
	head, _, ok := strings.Cut(id, ":")
	if !ok {
		return 0
	}
	n, err := strconv.ParseUint(head, 10, 64)
	if err != nil {
		return 0
	}
	return uint(n)
}

func metaUint(meta map[string]any, key string) uint {
	if meta == nil {
		return 0
	}
	switch n := meta[key].(type) {
	case uint:
		return n
	case int:
		return uint(n)
	case int64:
		return uint(n)
	case float64:
		return uint(n)
	default:
		return 0
	}
}
