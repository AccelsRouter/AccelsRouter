/*
Admin top-up (recharge order) types. Mirrors backend model.TopUp.
*/
export type TopUpOrder = {
  id: number
  user_id: number
  username: string
  amount: number // quota credited
  money: number // fiat/USD amount charged
  trade_no: string
  payment_method: string
  payment_provider: string
  create_time: number // unix seconds
  complete_time: number // unix seconds, 0 if not completed
  status: string // pending | success | ...
  remark: string
}

export type TopUpListResponse = {
  items: TopUpOrder[]
  total: number
  page: number
  page_size: number
}
