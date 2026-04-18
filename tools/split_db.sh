#!/bin/bash
set -e

SOURCE_DB=${1:-"hijarr.db"}
SRN_DB="srn.db"
STATE_DB="state.db"
CACHE_DB="cache.db"

if [ ! -f "$SOURCE_DB" ]; then
    echo "❌ 找不到源数据库文件: $SOURCE_DB"
    echo "用法: ./split_db.sh <源数据库路径>"
    exit 1
fi

echo "🚀 开始分离数据库: $SOURCE_DB"

# 1. 创建 SRN 数据库 (保留 srn_events，删除其他)
echo "📦 正在生成 $SRN_DB ..."
cp "$SOURCE_DB" "$SRN_DB"
sqlite3 "$SRN_DB" <<EOF
DROP TABLE IF EXISTS seen_files;
DROP TABLE IF EXISTS failed_files;
DROP TABLE IF EXISTS global_stats;
DROP TABLE IF EXISTS tried_magnets;
DROP TABLE IF EXISTS seen_rss;
DROP TABLE IF EXISTS torrent_patterns;
DROP TABLE IF EXISTS subtitle_cache;
VACUUM;
EOF

# 2. 创建 State 数据库 (保留状态表，删除 srn_events / subtitle_cache)
echo "📦 正在生成 $STATE_DB ..."
cp "$SOURCE_DB" "$STATE_DB"
sqlite3 "$STATE_DB" <<EOF
DROP TABLE IF EXISTS srn_events;
DROP TABLE IF EXISTS subtitle_cache;
VACUUM;
EOF

# 3. 如果需要的话，原 DB 也是 cache 库 (这里可以选作生成独立的 cache.db)
echo "📦 正在生成 $CACHE_DB ..."
cp "$SOURCE_DB" "$CACHE_DB"
sqlite3 "$CACHE_DB" <<EOF
DROP TABLE IF EXISTS srn_events;
DROP TABLE IF EXISTS seen_files;
DROP TABLE IF EXISTS failed_files;
DROP TABLE IF EXISTS global_stats;
DROP TABLE IF EXISTS tried_magnets;
DROP TABLE IF EXISTS seen_rss;
DROP TABLE IF EXISTS torrent_patterns;
VACUUM;
EOF

echo "✅ 数据库分离完成！"
echo "  - SRN 专用库   : $SRN_DB"
echo "  - State 专用库 : $STATE_DB"
echo "  - Cache 专用库 : $CACHE_DB (只保留 API 请求缓存)"
echo ""
echo "注意: 请在替换生产环境文件前妥善备份原 $SOURCE_DB 文件。"
