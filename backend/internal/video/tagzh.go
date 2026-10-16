package video

import (
	"regexp"
	"strings"
	"unicode"

	"gorm.io/gorm"
)

var tagToken = regexp.MustCompile(`#([^\s#]+)`)

// ChineseTags 把从标题里抽出的词换成中文标签。虚词、分辨率和纯数字丢掉。
func ChineseTags(tags []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		zh, ok := toChineseTag(tag)
		if !ok {
			continue
		}
		if _, dup := seen[zh]; dup {
			continue
		}
		seen[zh] = struct{}{}
		out = append(out, zh)
	}
	return out
}

func toChineseTag(tag string) (string, bool) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "", false
	}
	if hasHan(tag) {
		return tag, true
	}
	key := strings.ToLower(tag)
	if zh, ok := chineseTagNames[key]; ok {
		return zh, true
	}
	return "", false
}

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// RewriteStoredTags 把库里的英文标签改成中文，并同步视频描述里的 #标签。
func RewriteStoredTags(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	var tags []Tag
	if err := db.Find(&tags).Error; err != nil {
		return err
	}
	for _, tag := range tags {
		zh, ok := toChineseTag(tag.Name)
		if !ok {
			if err := db.Where("tag_id = ?", tag.ID).Delete(&VideoTag{}).Error; err != nil {
				return err
			}
			if err := db.Delete(&Tag{}, tag.ID).Error; err != nil {
				return err
			}
			continue
		}
		if zh == tag.Name {
			continue
		}
		var existing Tag
		err := db.Where("name = ?", zh).First(&existing).Error
		if err == nil && existing.ID != tag.ID {
			if err := db.Model(&VideoTag{}).Where("tag_id = ?", tag.ID).Update("tag_id", existing.ID).Error; err != nil {
				return err
			}
			if err := db.Delete(&Tag{}, tag.ID).Error; err != nil {
				return err
			}
			continue
		}
		if err := db.Model(&Tag{}).Where("id = ?", tag.ID).Update("name", zh).Error; err != nil {
			return err
		}
	}
	if err := db.Exec(`DELETE t1 FROM video_tags t1 INNER JOIN video_tags t2 ON t1.video_id = t2.video_id AND t1.tag_id = t2.tag_id AND t1.id > t2.id`).Error; err != nil {
		return err
	}

	var videos []Video
	if err := db.Select("id", "description").Where("description LIKE ?", "%#%").Find(&videos).Error; err != nil {
		return err
	}
	for _, item := range videos {
		next := rewriteDescriptionTags(item.Description)
		if next == item.Description {
			continue
		}
		if err := db.Model(&Video{}).Where("id = ?", item.ID).Update("description", next).Error; err != nil {
			return err
		}
	}
	return nil
}

func rewriteDescriptionTags(description string) string {
	replaced := tagToken.ReplaceAllStringFunc(description, func(token string) string {
		name := strings.TrimPrefix(token, "#")
		zh, ok := toChineseTag(name)
		if !ok {
			return ""
		}
		return "#" + zh
	})
	return strings.Join(strings.Fields(replaced), " ")
}

var chineseTagNames = map[string]string{
	"nature": "自然", "forest": "森林", "aerial": "航拍", "view": "景色", "tree": "树",
	"flower": "花", "flowers": "花", "white": "白色", "city": "城市", "sunset": "日落",
	"sky": "天空", "garden": "花园", "gardens": "花园", "abstract": "抽象",
	"airplane": "飞机", "plane": "飞机", "animal": "动物", "baby": "婴儿", "beer": "啤酒",
	"bicycle": "自行车", "bike": "自行车", "business": "商务", "car": "汽车",
	"beach": "海滩", "walking": "步行", "walk": "步行", "pedestrian": "步行",
	"water": "水", "rain": "雨", "rainy": "雨", "sports": "运动",
	"cat": "猫", "cats": "猫", "kitten": "猫", "christmas": "圣诞",
	"palm": "棕榈", "pink": "粉色", "blue": "蓝色", "rocky": "岩石",
	"traveling": "旅行", "seagulls": "鸟", "seabird": "鸟", "bird": "鸟", "birds": "鸟",
	"playing": "玩耍", "glass": "玻璃", "couple": "情侣", "waterfall": "瀑布",
	"meadow": "草地", "flying": "飞行", "beautiful": "美景", "cherry": "樱花",
	"background": "背景", "clouds": "云", "cloud": "云", "window": "窗户",
	"road": "道路", "serving": "服务", "cyclist": "骑行", "ride": "骑行",
	"person": "人", "people": "人", "close": "特写", "closeup": "特写",
	"wave": "海浪", "waves": "海浪", "breeze": "微风", "japanese": "日式",
	"blossom": "花开", "blossoms": "花开", "field": "田野", "sunflowers": "向日葵",
	"bright": "明亮", "orange": "橙色", "peaceful": "宁静", "sand": "沙滩",
	"stunning": "壮丽", "small": "小巧", "raft": "木筏", "camera": "镜头",
	"landscape": "风景", "path": "小路", "overhead": "俯拍", "wooden": "木质",
	"pier": "栈桥", "perspective": "视角", "roses": "玫瑰", "bouquet": "花束",
	"exploring": "探索", "flowery": "花海", "soft": "柔和", "shine": "光影",
	"tropical": "热带", "falling": "飘落", "tunnel": "隧道", "space": "星空",
	"stars": "星空", "liquid": "液体", "dream": "梦幻", "world": "世界",
	"crystal": "水晶", "curvy": "弯曲", "curved": "弯道", "mounds": "山丘",
	"cape": "海角", "dusk": "黄昏", "landing": "降落", "illuminated": "灯火",
	"caressing": "轻抚", "boardwalk": "木栈道", "green": "绿色",
	"chameleon": "变色龙", "camels": "骆驼", "desert": "沙漠", "lying": "躺卧",
	"born": "新生", "young": "年轻", "mother": "母亲", "portrait": "肖像",
	"smiling": "微笑", "little": "幼小", "vertical": "竖屏", "blazing": "炽热",
	"bartender": "调酒", "filling": "倒酒", "toasting": "干杯", "craft": "精酿",
	"silhouette": "剪影", "girl": "女孩", "zigzagging": "蜿蜒", "park": "公园",
	"singing": "歌唱", "flock": "鸟群", "horizon": "地平线", "working": "工作",
	"laptop": "笔记本", "work": "办公", "cash": "现金", "rotating": "旋转",
	"graphs": "图表", "hands": "双手", "typing": "打字", "luxury": "豪华",
	"accelerating": "加速", "dashboard": "仪表盘", "owner": "主人", "cute": "可爱",
	"gray": "灰色", "hiding": "躲藏", "snow": "雪", "snowing": "雪", "foggy": "雾",
	"fireplace": "壁炉", "particle": "粒子", "bokeh": "光斑", "train": "列车",
	"driving": "行驶", "night": "夜晚", "tokyo": "东京", "moving": "移动",
	"mountainous": "山峦", "area": "地区", "alps": "阿尔卑斯", "traffic": "车流",
	"countryside": "乡村", "relaxing": "放松", "sunny": "晴天", "highway": "公路",
	"test": "测试", "slowly": "缓慢", "gigantic": "广阔", "covered": "覆盖",
	"slides": "滑行", "taking": "拍摄", "bottom": "水底", "screaming": "啼哭",
	"starts": "出发", "exposing": "展示", "side": "侧面",
}
