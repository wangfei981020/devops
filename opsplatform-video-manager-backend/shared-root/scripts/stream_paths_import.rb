#!/usr/bin/env ruby
# frozen_string_literal: true

# ruby scripts/stream_paths_import.rb
# 勿把含真实 token 的脚本提交到公共仓库；过期请重新登录替换 TOKEN。

require 'csv'
require 'json'
require 'net/http'
require 'uri'

BASE = 'http://localhost:3000'
TOKEN = 'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoxLCJ1c2VybmFtZSI6ImFkbWluIiwiaXNfYWRtaW4iOnRydWUsImlzcyI6InZpZGVvLW1hbmFnZXIiLCJzdWIiOiJhZG1pbiIsImV4cCI6NDkyNzkyMjA5MCwibmJmIjoxNzc0MzIyMDkwLCJpYXQiOjE3NzQzMjIwOTB9.qDkHD3QUxZAmzPd1n5mECNxNLoOr5yXrQu0vuib2wKY'
# 每行: :table_id, :full_path，以及 :stream_name 或 :stream_id（二选一即可）
ROWS = [
  { table_id: 'T01', full_path: '/live/a', stream_name: '欧洲二区' },
  { table_id: 'T02', full_path: '/live/b', stream_id: 1 }
].freeze

csv = CSV.generate(write_headers: true, headers: %w[桌台号 路径 流区域 stream_id], encoding: 'UTF-8') do |g|
  ROWS.each { |r| g << [r[:table_id], r[:full_path], r[:stream_name].to_s, r[:stream_id]&.to_s || ''] }
end

boundary = "----bm#{rand(999_999)}"
body = "--#{boundary}\r\n" \
       "Content-Disposition: form-data; name=\"file\"; filename=\"import.csv\"\r\n" \
       "Content-Type: text/csv\r\n\r\n#{csv}\r\n--#{boundary}--\r\n"

uri = URI.parse(BASE).merge('/api/stream-paths/import')
http = Net::HTTP.new(uri.host, uri.port)
http.use_ssl = (uri.scheme == 'https')
req = Net::HTTP::Post.new(uri.request_uri)
req['Authorization'] = "Bearer #{TOKEN.strip}"
req['Content-Type'] = "multipart/form-data; boundary=#{boundary}"
req.body = body

res = http.request(req)
abort res.body unless res.is_a?(Net::HTTPSuccess)

out = JSON.parse(res.body) rescue abort(res.body)
puts JSON.pretty_generate(out)

d = out['data'] || {}
errs = d['errors'] || []
warn "created=#{d['created']} updated=#{d['updated']} errors=#{errs.size}"
exit 1 if out['code'].to_i != 200 || !errs.empty?
