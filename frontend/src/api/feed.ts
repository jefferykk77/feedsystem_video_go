import { getQuery } from './client'
import { normalizeFeedVideoList } from './normalize'
import type { ListByFollowingResponse, ListByPopularityResponse, ListLatestResponse, ListLikesCountResponse, ListRecommendResponse } from './types'

export async function listRecommend(input: { source: 'vector' | 'following' | 'hot'; offset?: number; latest_time?: number; exclude?: string }) {
  const query: Record<string, string | number | undefined> = { source: input.source }
  if (typeof input.offset === 'number') query.offset = input.offset
  if (typeof input.latest_time === 'number' && input.latest_time > 0) query.latest_time = input.latest_time
  if (input.exclude) query.exclude = input.exclude
  const res = await getQuery<ListRecommendResponse>('/feed/listRecommend', query)
  return { ...res, video_list: normalizeFeedVideoList(res.video_list) }
}

export async function listLatest(input: { limit: number; latest_time: number }) {
  const res = await getQuery<ListLatestResponse>('/feed/listLatest', input)
  return { ...res, video_list: normalizeFeedVideoList(res.video_list) }
}

export async function listLikesCount(input: { limit: number; likes_count_before?: number; id_before?: number }) {
  const query: Record<string, number> = { limit: input.limit }
  if (typeof input.likes_count_before === 'number' || typeof input.id_before === 'number') {
    query.likes_count_before = input.likes_count_before ?? 0
    query.id_before = input.id_before ?? 0
  }
  const res = await getQuery<ListLikesCountResponse>('/feed/listLikesCount', query)
  return { ...res, video_list: normalizeFeedVideoList(res.video_list) }
}

export async function listByPopularity(input: { limit: number; as_of: number; offset: number }) {
  const res = await getQuery<ListByPopularityResponse>('/feed/listByPopularity', input)
  return { ...res, video_list: normalizeFeedVideoList(res.video_list) }
}

export async function listByFollowing(input: { limit: number; latest_time: number }) {
  const res = await getQuery<ListByFollowingResponse>('/feed/listByFollowing', input, { authRequired: true })
  return { ...res, video_list: normalizeFeedVideoList(res.video_list) }
}
