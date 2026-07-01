# frozen_string_literal: true

# Copyright (c) 2025 kk
#
# This software is released under the MIT License.
# https://opensource.org/licenses/MIT


require 'bundler/setup'
require 'kk/git/rake_tasks'

task default: %w[push]

task :push do
  branch = `git rev-parse --abbrev-ref HEAD`.strip
  ENV['KK_GIT_BRANCH'] = branch
  # Explicit remote/branch avoids "Cannot fast-forward to multiple branches"
  # when fetch runs concurrently (e.g. IDE autofetch) with bare `git pull --ff-only`.
  ENV['KK_GIT_PULL_ARGS'] = "origin #{branch} --ff-only"

  if `git status --porcelain`.strip.empty?
    ahead = `git rev-list --count @{u}..HEAD 2>/dev/null`.to_i
    if ahead.positive?
      ok = system('git', 'push', 'origin', branch)
      raise 'git push failed' unless ok

      puts "Pushed #{ahead} commit(s) to origin #{branch}"
      next
    end
  end

  Rake::Task['git:auto_commit_push'].invoke
end

task :run do
  system 'docker compose down -v'
  system 'docker compose up -d --build --remove-orphans'
  system 'docker compose logs -f'
end
