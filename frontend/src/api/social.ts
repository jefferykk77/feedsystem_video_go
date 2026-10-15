import { getQuery, postJson } from './client'
import { listOrEmpty, normalizeAccount } from './normalize'
import type { GetAllFollowersResponse, GetAllVloggersResponse, MessageResponse } from './types'

export function follow(vloggerId: number) {
  return postJson<MessageResponse>('/social/follow', { vlogger_id: vloggerId }, { authRequired: true })
}

export function unfollow(vloggerId: number) {
  return postJson<MessageResponse>('/social/unfollow', { vlogger_id: vloggerId }, { authRequired: true })
}

export async function getAllFollowers(vloggerId?: number) {
  const res = await getQuery<GetAllFollowersResponse>(
    '/social/getAllFollowers',
    vloggerId ? { vlogger_id: vloggerId } : undefined,
    { authRequired: true },
  )
  return { ...res, followers: listOrEmpty(res.followers).map(normalizeAccount) }
}

export async function getAllVloggers(followerId?: number) {
  const res = await getQuery<GetAllVloggersResponse>(
    '/social/getAllVloggers',
    followerId ? { follower_id: followerId } : undefined,
    { authRequired: true },
  )
  return { ...res, vloggers: listOrEmpty(res.vloggers).map(normalizeAccount) }
}
