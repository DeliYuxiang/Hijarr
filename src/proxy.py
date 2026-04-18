import os
import httpx
import json
import xml.etree.ElementTree as ET
from fastapi import FastAPI, Request, Response
from typing import Any, Dict

app = FastAPI()

# --- 环境配置 ---
PROWLARR_TARGET_URL = os.getenv("PROWLARR_TARGET_URL",
                                "http://prowlarr:9696/2/api")
PROWLARR_API_KEY = os.getenv("PROWLARR_API_KEY", "")
TMDB_API_KEY = os.getenv("TMDB_API_KEY", "")
TARGET_LANGUAGE = os.getenv("TARGET_LANGUAGE", "zh-CN")
TVDB_LANGUAGE = os.getenv("TVDB_LANGUAGE", "zho")

ANIME_SEASON_MAP = {
    "物语系列": {
        "include_title": False,
        "seasons": {
            "1": "化物语",
            "2": "伪物语",
            "3": "第二季",
            "4": "终物语",
            "5": "终物语SP",
            "6": "第外季&第怪季"
        }
    },
    "鬼灭之刃": {
        "include_title": True,
        "seasons": {
            "2": "游郭篇 无限列车篇",
            "3": "刀匠村篇",
            "4": "柱训练篇"
        }
    },
    "诸神之黄昏": {
        "include_title": True,
        "seasons": {
            "2": "貳之章"
        }
    }
}

CACHE = {}


async def fetch_tmdb_series_info(external_id: str,
                                 source: str) -> Dict[str, Any]:
    if not TMDB_API_KEY: return None
    cache_key = f"{source}_{external_id}_{TARGET_LANGUAGE}"
    if cache_key in CACHE: return CACHE[cache_key]

    url = f"https://api.themoviedb.org/3/find/{external_id}"
    params = {
        "api_key": TMDB_API_KEY,
        "external_source": source,
        "language": TARGET_LANGUAGE
    }
    async with httpx.AsyncClient() as client:
        try:
            resp = await client.get(url, params=params, timeout=10.0)
            data = resp.json()
            results = data.get("tv_results") or data.get("movie_results")
            if results and len(results) > 0:
                title = results[0].get("name") or results[0].get("title")
                tmdb_id = results[0].get("id")
                if title:
                    info = {"title": title, "tmdb_id": tmdb_id}
                    CACHE[cache_key] = info
                    return info
            return None
        except Exception:
            return None


async def fetch_tmdb_episode_titles(tmdb_id: int,
                                    season_number: int) -> Dict[int, str]:
    if not TMDB_API_KEY: return {}
    cache_key = f"episodes_{tmdb_id}_{season_number}_{TARGET_LANGUAGE}"
    if cache_key in CACHE: return CACHE[cache_key]

    url = f"https://api.themoviedb.org/3/tv/{tmdb_id}/season/{season_number}"
    params = {"api_key": TMDB_API_KEY, "language": TARGET_LANGUAGE}
    async with httpx.AsyncClient() as client:
        try:
            resp = await client.get(url, params=params, timeout=10.0)
            data = resp.json()
            episodes = data.get("episodes", [])
            mapping = {ep["episode_number"]: ep["name"] for ep in episodes}
            if mapping:
                CACHE[cache_key] = mapping
            return mapping
        except Exception:
            return {}


async def fetch_chinese_title_by_query(query: str) -> str:
    if not TMDB_API_KEY: return None
    cache_key = f"query_{query}_{TARGET_LANGUAGE}"
    if cache_key in CACHE: return CACHE[cache_key]

    url = "https://api.themoviedb.org/3/search/tv"
    params = {
        "api_key": TMDB_API_KEY,
        "query": query,
        "language": TARGET_LANGUAGE,
        "page": 1
    }
    async with httpx.AsyncClient() as client:
        try:
            resp = await client.get(url, params=params, timeout=10.0)
            data = resp.json()
            if data.get("results") and len(data["results"]) > 0:
                title = data["results"][0].get("name")
                CACHE[cache_key] = title
                return title
            return None
        except Exception:
            return None


# ==========================================
# 🚨 核心：TVDB 透明劫持代理 (MITM)
# ==========================================
@app.api_route("/{path:path}",
               methods=["GET", "POST", "PUT", "DELETE", "HEAD", "OPTIONS"])
async def tvdb_mitm_proxy(path: str, request: Request):
    """
    拦截所有发往 api.thetvdb.com 的请求
    由于配置了 DNS 劫持，Sonarr 的请求会打到这里。
    """
    # 避免拦截我们自己的 /api (Torznab)
    if path.startswith("api"):
        return await torznab_proxy(request)

    # 1. 劫持语言设置：如果请求指定了非目标语言，强制替换为 TVDB_LANGUAGE
    # 匹配 /v4/series/{id}/episodes/{type}/{lang} 或 /v4/series/{id}/{lang} 等
    parts = path.split("/")
    lang_modified = False
    if len(parts) >= 3:
        # TVDB v4 常见的语言位在最后或倒数第二位
        for i in [-1, -2]:
            if abs(i) > len(parts): continue
            if len(parts[i]) == 3 and parts[i].isalpha():
                if parts[i] != TVDB_LANGUAGE:
                    print(f"🌐 [语言劫持] {parts[i]} -> {TVDB_LANGUAGE}")
                    parts[i] = TVDB_LANGUAGE
                    lang_modified = True
                break

    new_path = "/".join(parts) if lang_modified else path

    # 真实的 TVDB/Skyhook API 地址
    request_host = request.headers.get("host", "api.thetvdb.com")
    if "sonarr.tv" in request_host:
        target_host = "skyhook.sonarr.tv"
    else:
        target_host = request_host if "thetvdb.com" in request_host else "api.thetvdb.com"

    real_url = f"https://{target_host}/{new_path}"
    if request.url.query:
        real_url += f"?{request.url.query}"

    headers = {
        k: v
        for k, v in request.headers.items()
        if k.lower() not in ["host", "content-length", "accept-encoding"]
    }
    headers["Host"] = target_host

    body = await request.body()

    async with httpx.AsyncClient(verify=False) as client:
        try:
            proxy_resp = await client.request(method=request.method,
                                              url=real_url,
                                              headers=headers,
                                              content=body,
                                              timeout=20.0)

            content_type = proxy_resp.headers.get("content-type", "")

            if "application/json" in content_type and proxy_resp.status_code == 200:
                try:
                    data = proxy_resp.json()
                    modified = False

                    # --- 处理 Skyhook (Sonarr 专用格式) ---
                    # 1. 详情接口: /v1/tvdb/shows/{lang}/{tvdbId}
                    if "v1/tvdb/shows/" in path:
                        tvdb_id = data.get("tvdbId")
                        if tvdb_id:
                            tmdb_info = await fetch_tmdb_series_info(
                                str(tvdb_id), "tvdb_id")
                            if tmdb_info:
                                data["title"] = tmdb_info["title"]
                                tmdb_id_val = tmdb_info["tmdb_id"]

                                # 翻译单集
                                if "episodes" in data and isinstance(
                                        data["episodes"], list):
                                    episodes = data["episodes"]
                                    seasons = set(
                                        ep.get("seasonNumber")
                                        for ep in episodes
                                        if ep.get("seasonNumber") is not None)
                                    ep_mapping = {}
                                    for s_num in seasons:
                                        m = await fetch_tmdb_episode_titles(
                                            tmdb_id_val, s_num)
                                        ep_mapping[s_num] = m

                                    for ep in episodes:
                                        s_num = ep.get("seasonNumber")
                                        e_num = ep.get("episodeNumber")
                                        if s_num in ep_mapping and e_num in ep_mapping[
                                                s_num]:
                                            ep["title"] = ep_mapping[s_num][
                                                e_num]

                                modified = True
                                print(f"💀 [Skyhook 详情] ID: {tvdb_id} 已翻译")

                    # 2. 搜索接口: /v1/tvdb/search/{lang}/?term=...
                    elif "v1/tvdb/search/" in path:
                        if isinstance(data, list):
                            for item in data:
                                tvdb_id = item.get("tvdbId")
                                if tvdb_id:
                                    tmdb_info = await fetch_tmdb_series_info(
                                        str(tvdb_id), "tvdb_id")
                                    if tmdb_info:
                                        item["title"] = tmdb_info["title"]
                                        modified = True
                            if modified:
                                print(f"💀 [Skyhook 搜索] 结果已批量翻译")

                    # --- 处理原生 TVDB v4 ---
                    elif path.startswith("v4/series/") and "data" in data:
                        # ... (保持原有的 TVDB 翻译逻辑)

                        # 提取 Series ID (可能在 data 直接下面或在 data.series 下面)
                        series_data = data["data"].get("series", data["data"])
                        tvdb_id = series_data.get("id")

                        if tvdb_id:
                            tmdb_info = await fetch_tmdb_series_info(
                                str(tvdb_id), "tvdb_id")

                            if tmdb_info:
                                chinese_title = tmdb_info["title"]
                                tmdb_id = tmdb_info["tmdb_id"]

                                # 翻译剧集名
                                if "name" in series_data:
                                    series_data["name"] = chinese_title
                                    modified = True

                                # 强制覆盖翻译列表
                                if "translations" in series_data:
                                    series_data["translations"][
                                        "nameTranslations"] = [{
                                            "name":
                                            chinese_title,
                                            "language":
                                            TVDB_LANGUAGE
                                        }]
                                    modified = True

                                # 翻译剧集列表 (Episodes)
                                if "episodes" in data["data"] and isinstance(
                                        data["data"]["episodes"], list):
                                    episodes = data["data"]["episodes"]
                                    # 按季分组以优化请求
                                    seasons = set(
                                        ep.get("seasonNumber")
                                        for ep in episodes
                                        if ep.get("seasonNumber") is not None)

                                    ep_mapping = {}
                                    for s_num in seasons:
                                        m = await fetch_tmdb_episode_titles(
                                            tmdb_id, s_num)
                                        ep_mapping[s_num] = m

                                    for ep in episodes:
                                        s_num = ep.get("seasonNumber")
                                        e_num = ep.get("number")
                                        if s_num in ep_mapping and e_num in ep_mapping[
                                                s_num]:
                                            ep["name"] = ep_mapping[s_num][
                                                e_num]
                                            modified = True

                                if modified:
                                    print(
                                        f"💀 [TVDB 劫持] ID: {tvdb_id} 数据已篡改 (TMDB ID: {tmdb_id})"
                                    )

                    # TVDB v4 搜索接口: /v4/search
                    elif path.startswith(
                            "v4/search") and "data" in data and isinstance(
                                data["data"], list):
                        for item in data["data"]:
                            tvdb_id = item.get("tvdb_id")
                            if tvdb_id:
                                tmdb_info = await fetch_tmdb_series_info(
                                    str(tvdb_id), "tvdb_id")
                                if tmdb_info:
                                    item["name"] = tmdb_info["title"]
                                    modified = True
                        if modified:
                            print(f"💀 [TVDB 劫持] 搜索结果已批量篡改")

                    if modified:
                        return Response(content=json.dumps(data),
                                        status_code=200,
                                        media_type="application/json")

                except Exception as json_e:
                    print(f"👀 [JSON 解析跳过] {json_e}")

            # 原样透传
            excluded_headers = [
                "content-encoding", "transfer-encoding", "content-length",
                "connection"
            ]
            return Response(content=proxy_resp.content,
                            status_code=proxy_resp.status_code,
                            headers={
                                k: v
                                for k, v in proxy_resp.headers.items()
                                if k.lower() not in excluded_headers
                            },
                            media_type=content_type)
        except Exception as e:
            print(f"❌ TVDB Proxy 转发失败: {e}")
            return Response(content=f'{{"error": "{str(e)}"}}',
                            status_code=500,
                            media_type="application/json")


# ==========================================
# 🚀 Torznab Proxy (保持原样)
# ==========================================
async def torznab_proxy(request: Request):
    params = dict(request.query_params)
    search_type = params.get("t")
    original_q = params.get("q", "")

    if search_type in ["tvsearch", "movie"]:
        chinese_title = None
        if "tvdbid" in params:
            tmdb_info = await fetch_tmdb_series_info(params["tvdbid"],
                                                     "tvdb_id")
            if tmdb_info:
                chinese_title = tmdb_info["title"]
                del params["tvdbid"]
        elif "imdbid" in params:
            tmdb_info = await fetch_tmdb_series_info(params["imdbid"],
                                                     "imdb_id")
            if tmdb_info:
                chinese_title = tmdb_info["title"]
                del params["imdbid"]
        elif "q" in params and params["q"].strip():
            chinese_title = await fetch_chinese_title_by_query(params["q"])

        if chinese_title:
            params["q"] = chinese_title
            print(f"🚀 [代理介入] 投喂关键字: '{params['q']}'")

        if chinese_title or ("q" in params):
            current_q = params.get("q", chinese_title or "")
            if current_q in ANIME_SEASON_MAP:
                s_map = ANIME_SEASON_MAP[current_q]
                if "season" in params:
                    s_num = params["season"]
                    params[
                        "q"] = f"{current_q} {s_map['seasons'][s_num]}" if s_map[
                            "include_title"] else s_map['seasons'][s_num]
            else:
                suffix = ""
                if "season" in params:
                    s_num = params["season"]
                    q_lower = current_q.lower()
                    patterns = [
                        f"s{s_num}", f"s{s_num.zfill(2)}", f"第{s_num}季",
                        f"season {s_num}"
                    ]
                    if not any(p in q_lower for p in patterns):
                        suffix += f" S{s_num}"
                if "ep" in params:
                    e_num = params["ep"]
                    e_patterns = [
                        f"e{e_num}", f"e{e_num.zfill(2)}", f"第{e_num}集"
                    ]
                    if not any(p in q_lower for p in e_patterns):
                        suffix += f" E{e_num.zfill(2)}"
                if suffix:
                    params["q"] = f"{current_q}{suffix}".strip()

                for p in ["season", "ep"]:
                    if p in params: del params[p]

    forward_params = {**params, "apikey": PROWLARR_API_KEY}
    async with httpx.AsyncClient() as client:
        try:
            current_q = forward_params.get("q")
            if current_q:
                resp = await client.get(PROWLARR_TARGET_URL,
                                        params=forward_params,
                                        timeout=15.0)
                root = ET.fromstring(resp.content)
                items = root.findall('./channel/item')

                if len(items) < 10 and " S" in current_q:
                    forward_params["q"] = current_q.split(" S")[0]
                    resp = await client.get(PROWLARR_TARGET_URL,
                                            params=forward_params,
                                            timeout=15.0)
                    root = ET.fromstring(resp.content)
                    items = root.findall('./channel/item')

                if len(items) < 10 and original_q and forward_params[
                        "q"] != original_q:
                    forward_params["q"] = original_q
                    resp = await client.get(PROWLARR_TARGET_URL,
                                            params=forward_params,
                                            timeout=15.0)
                    root = ET.fromstring(resp.content)
                    items = root.findall('./channel/item')
            else:
                resp = await client.get(PROWLARR_TARGET_URL,
                                        params=forward_params,
                                        timeout=15.0)
                root = ET.fromstring(resp.content)
                items = root.findall('./channel/item')

            if items:
                for item in items:
                    guid_elem = item.find('guid')
                    magnet_link = None
                    if guid_elem is not None and guid_elem.text and guid_elem.text.startswith(
                            'magnet:'):
                        magnet_link = guid_elem.text

                    if magnet_link:
                        if item.find('link') is not None:
                            item.find('link').text = magnet_link
                        enclosure = item.find('enclosure')
                        if enclosure is not None:
                            enclosure.set('url', magnet_link)
                            enclosure.set('type', 'application/x-bittorrent')
                    else:
                        for tag in ['link', 'enclosure']:
                            elem = item.find(tag)
                            if elem is not None:
                                url_val = elem.text if tag == 'link' else elem.get(
                                    'url')
                                if url_val and "&file=" in url_val:
                                    new_url = url_val.split("&file=")[0]
                                    if tag == 'link': elem.text = new_url
                                    else: elem.set('url', new_url)

                return Response(content=ET.tostring(root, encoding='utf-8'),
                                media_type="application/rss+xml")

            return Response(content=resp.content,
                            media_type=resp.headers.get("content-type"))
        except Exception as e:
            return Response(content=f"<error>{e}</error>", status_code=500)
