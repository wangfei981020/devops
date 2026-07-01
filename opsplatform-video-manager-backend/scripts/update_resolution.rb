#!/usr/bin/env ruby
# frozen_string_literal: true

# Copyright (c) 2025 kk
#
# This software is released under the MIT License.
# https://opensource.org/licenses/MIT

# Script to update resolution for existing video stream endpoints
# Usage: ruby update_resolution.rb

require 'net/http'
require 'json'
require 'uri'

API_BASE_URL = ENV['API_BASE_URL'] || 'http://localhost:8080/api'
TOKEN = ENV['TOKEN'] || ''

def detect_resolution_from_path(full_path)
  path = full_path.upcase
  
  return '超清' if path.include?('UHD') || path.include?('4K')
  return '高清' if path.include?('HD')
  return '普清' if path.include?('SD')
  
  '普清' # default
end

def make_request(path, params = {})
  uri = URI("#{API_BASE_URL}#{path}")
  uri.query = URI.encode_www_form(params) unless params.empty?
  
  http = Net::HTTP.new(uri.host, uri.port)
  request = Net::HTTP::Get.new(uri.request_uri)
  request['Authorization'] = "Bearer #{TOKEN}" unless TOKEN.empty?
  
  response = http.request(request)
  
  unless response.code == '200'
    raise "API request failed: #{response.code} - #{response.body}"
  end
  
  JSON.parse(response.body)
end

def update_resolution(endpoint_id, resolution)
  uri = URI("#{API_BASE_URL}/video-stream-endpoints/#{endpoint_id}/test-resolution")
  
  http = Net::HTTP.new(uri.host, uri.port)
  request = Net::HTTP::Post.new(uri.request_uri)
  request['Authorization'] = "Bearer #{TOKEN}" unless TOKEN.empty?
  request['Content-Type'] = 'application/json'
  
  response = http.request(request)
  
  unless response.code == '200'
    raise "Update failed: #{response.code} - #{response.body}"
  end
  
  JSON.parse(response.body)
end

def main
  puts "Fetching all video stream endpoints..."
  data = make_request('/video-stream-endpoints')
  endpoints = data['data'] || []
  
  puts "Found #{endpoints.length} endpoints"
  puts "Updating resolution based on stream path..."
  
  updated = 0
  skipped = 0
  errors = 0
  
  endpoints.each_with_index do |endpoint, index|
    stream_path = endpoint.dig('stream_path', 'full_path')
    next unless stream_path
    
    detected = detect_resolution_from_path(stream_path)
    current = endpoint['resolution'] || '普清'
    
    if detected == current
      skipped += 1
      next
    end
    
    begin
      update_resolution(endpoint['id'], detected)
      updated += 1
      puts "[#{index + 1}/#{endpoints.length}] Updated endpoint #{endpoint['id']}: #{current} -> #{detected}"
    rescue => e
      errors += 1
      puts "[#{index + 1}/#{endpoints.length}] Error updating endpoint #{endpoint['id']}: #{e.message}"
    end
  end
  
  puts "\nSummary:"
  puts "  Updated: #{updated}"
  puts "  Skipped: #{skipped}"
  puts "  Errors: #{errors}"
end

main if __FILE__ == $PROGRAM_NAME

