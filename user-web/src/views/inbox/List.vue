<template>
  <div class="inbox-page">
    <el-card
      class="header-card"
      shadow="never"
    >
      <div class="page-header">
        <div class="header-text">
          <h2>{{ $t('统一收件箱') }}</h2>
          <p class="subtitle">
            {{ $t('跨渠道会话工作台：未读跟踪、坐席分配、对话线程集中处理') }}
          </p>
        </div>
        <div class="header-actions">
          <el-button @click="loadStats">
            <el-icon><DataAnalysis /></el-icon>
            {{ $t('刷新统计') }}
          </el-button>
          <el-button
            v-if="userStore.isAdmin"
            type="warning"
            plain
            @click="handleReconcile('backfill')"
          >
            <el-icon><Refresh /></el-icon>
            {{ $t('会话补建') }}
          </el-button>
          <el-button
            type="primary"
            @click="refreshAll"
          >
            <el-icon><RefreshRight /></el-icon>
            {{ $t('刷新') }}
          </el-button>
        </div>
      </div>
      <div
        v-if="stats"
        class="stats-row"
      >
        <el-tag
          type="info"
          effect="plain"
        >
          {{ $t('总') }}: {{ stats.total ?? 0 }}
        </el-tag>
        <el-tag
          type="danger"
          effect="plain"
        >
          {{ $t('未读') }}: {{ stats.unread ?? 0 }}
        </el-tag>
        <el-tag
          type="warning"
          effect="plain"
        >
          {{ $t('待处理') }}: {{ stats.open ?? 0 }}
        </el-tag>
        <el-tag
          type="primary"
          effect="plain"
        >
          {{ $t('已分配') }}: {{ stats.assigned ?? 0 }}
        </el-tag>
        <el-tag
          type="success"
          effect="plain"
        >
          {{ $t('已关闭') }}: {{ stats.closed ?? 0 }}
        </el-tag>
        <el-tag
          v-if="(stats.overdue_count || 0) > 0"
          type="danger"
          effect="dark"
        >
          {{ $t('超时未响应') }}: {{ stats.overdue_count }}
        </el-tag>
      </div>
    </el-card>

    <div class="workbench">
      <!-- 左：会话列表 -->
      <el-card
        class="conv-panel"
        shadow="never"
      >
        <div class="conv-filters">
          <ChannelSelect
            v-model="searchForm.platform"
            :placeholder="$t('全部平台')"
            size="default"
            style="width: 130px"
            @change="handleSearch"
          />
          <el-select
            v-model="searchForm.account_id"
            :placeholder="$t('inboxT10.allAccounts')"
            clearable
            filterable
            style="width: 140px"
            @change="handleSearch"
          >
            <el-option
              v-for="a in accountOptions"
              :key="a.value"
              :label="a.label"
              :value="a.value"
            />
          </el-select>
          <el-select
            v-model="searchForm.conversation_type"
            :placeholder="$t('inboxT10.allTypes')"
            clearable
            style="width: 130px"
            @change="handleSearch"
          >
            <el-option
              :label="$t('inboxT10.singleChat')"
              value="single"
            />
            <el-option
              :label="$t('inboxT10.groupChat')"
              value="group"
            />
          </el-select>
          <el-input
            v-model="searchForm.nickname"
            :placeholder="$t('inboxT10.nicknamePlaceholder')"
            clearable
            style="width: 150px"
            @keyup.enter="handleSearch"
          />
          <el-select
            v-model="searchForm.status"
            :placeholder="$t('全部状态')"
            clearable
            style="width: 120px"
            @change="handleSearch"
          >
            <el-option
              :label="$t('未读')"
              value="unread"
            />
            <el-option
              :label="$t('待处理')"
              value="open"
            />
            <el-option
              :label="$t('已分配')"
              value="assigned"
            />
            <el-option
              :label="$t('已关闭')"
              value="closed"
            />
            <el-option
              :label="$t('稍后处理')"
              value="snoozed"
            />
          </el-select>
          <el-select
            v-model="searchForm.filter"
            :placeholder="$t('语义筛选')"
            clearable
            style="width: 130px"
            @change="handleSearch"
          >
            <el-option
              :label="$t('待首响')"
              value="awaiting_first_reply"
            />
          </el-select>
          <el-select
            v-model="searchForm.pinned"
            :placeholder="$t('置顶筛选')"
            clearable
            style="width: 120px"
            @change="handleSearch"
          >
            <el-option
              :label="$t('仅置顶')"
              value="true"
            />
            <el-option
              :label="$t('未置顶')"
              value="false"
            />
          </el-select>
          <el-input
            v-model="searchForm.keyword"
            :placeholder="$t('客户名/ID')"
            clearable
            style="flex: 1; min-width: 130px"
            @keyup.enter="handleSearch"
          >
            <template #append>
              <el-button
                :icon="Search"
                @click="handleSearch"
              />
            </template>
          </el-input>
        </div>

        <div
          v-loading="loading"
          class="conv-list"
        >
          <div
            v-if="singleConversations.length"
            class="conv-section-title"
          >
            {{ $t('inboxT10.singleChat') }}
          </div>
          <div
            v-for="conv in singleConversations"
            :key="conv.id"
            class="conv-item"
            role="button"
            tabindex="0"
            :class="{ active: currentConv && currentConv.id === conv.id }"
            @click="selectConversation(conv)"
            @keydown.enter.prevent="selectConversation(conv)"
            @keydown.space.prevent="selectConversation(conv)"
          >
            <div class="conv-item-top">
              <span class="conv-name">
                <!-- M-2 客户头像链路：会话列表展示客户头像，空头像降级为默认人像图标 -->
                <el-avatar
                  :size="22"
                  :src="conv.sender_avatar || ''"
                  class="conv-avatar"
                >
                  <el-icon><User /></el-icon>
                </el-avatar>
                <el-icon
                  v-if="conv.pinned"
                  class="pin-icon"
                ><Top /></el-icon>
                <el-icon
                  v-if="conv.muted"
                  class="mute-icon"
                ><Mute /></el-icon>
                {{ conv.customer_name || conv.customer_id || '-' }}
              </span>
              <span class="conv-time">{{ formatTime(conv.last_message_at) }}</span>
            </div>
            <div class="conv-item-mid">
              <span class="conv-preview">{{ conv.last_message_preview || $t('暂无消息') }}</span>
              <el-badge
                v-if="conv.unread_count > 0"
                :value="conv.unread_count"
                :max="99"
                class="conv-badge"
              />
            </div>
            <div class="conv-item-bottom">
              <el-tag
                size="small"
                :type="getChannelTagType(conv.platform) || 'info'"
              >
                {{ catalog.channelLabel(conv.platform) || conv.platform }}
              </el-tag>
              <el-tag
                size="small"
                type="info"
                effect="plain"
              >
                {{ $t('inboxT10.singleChat') }}
              </el-tag>
              <el-tag
                v-if="conv.account_id"
                size="small"
                type="info"
                effect="plain"
              >
                {{ catalog.accountLabel(conv.account_id) }}
              </el-tag>
              <el-tag
                size="small"
                :type="statusTagType(conv.status)"
              >
                {{ statusLabel(conv.status) }}
              </el-tag>
              <!-- 「待首响」等待徽标（specs/024 T606）：只对客户在等、尚未首响的会话展示。 -->
              <el-tag
                v-if="waitingBadge(conv)"
                size="small"
                type="danger"
                effect="plain"
              >
                {{ waitingBadge(conv) }}
              </el-tag>
              <el-tag
                v-if="conv.assigned_to"
                size="small"
                type="primary"
                effect="plain"
              >
                {{ $t('坐席') }}: {{ conv.assigned_to }}
              </el-tag>
              <el-tag
                v-for="tag in (conv.tags || []).slice(0, 3)"
                :key="tag"
                size="small"
                type="warning"
                effect="plain"
                closable
                @close.stop="handleRemoveTag(conv, tag)"
              >
                {{ tag }}
              </el-tag>
              <el-icon
                v-if="conv.starred"
                class="star-icon"
                @click.stop="handleToggleStar(conv)"
              >
                <StarFilled />
              </el-icon>
            </div>
          </div>

          <div
            v-if="groupConversations.length"
            class="conv-section-title"
          >
            {{ $t('inboxT10.groupChat') }}
          </div>
          <div
            v-for="card in groupConversations"
            :key="`group-${card.group_key}`"
            class="conv-item conv-group-item"
            role="button"
            tabindex="0"
            :class="{ active: currentConv && currentConv.group_key === card.group_key }"
            @click="selectGroupCard(card)"
            @keydown.enter.prevent="selectGroupCard(card)"
            @keydown.space.prevent="selectGroupCard(card)"
          >
            <div class="conv-item-top">
              <span class="conv-name">
                <el-avatar
                  :size="22"
                  :src="card.sender_avatar || ''"
                  class="conv-avatar"
                >
                  <el-icon><User /></el-icon>
                </el-avatar>
                <el-icon
                  v-if="card.pinned"
                  class="pin-icon"
                ><Top /></el-icon>
                {{ card.group_name }}
              </span>
              <span class="conv-time">{{ formatTime(card.last_message_at) }}</span>
            </div>
            <div class="conv-item-mid">
              <span class="conv-preview">{{ card.last_message_preview || $t('暂无消息') }}</span>
              <el-badge
                v-if="card.unread_count > 0"
                :value="card.unread_count"
                :max="99"
                class="conv-badge"
              />
            </div>
            <div class="conv-item-bottom">
              <el-tag
                size="small"
                type="warning"
                effect="plain"
              >
                {{ $t('inboxT10.groupChat') }} · {{ card.member_count }} {{ $t('inboxT10.members') }}
              </el-tag>
              <el-tag
                size="small"
                :type="getChannelTagType(card.platform) || 'info'"
              >
                {{ catalog.channelLabel(card.platform) || card.platform }}
              </el-tag>
              <el-tag
                v-if="card.account_id"
                size="small"
                type="info"
                effect="plain"
              >
                {{ catalog.accountLabel(card.account_id) }}
              </el-tag>
              <el-tag
                size="small"
                :type="statusTagType(card.status)"
              >
                {{ statusLabel(card.status) }}
              </el-tag>
            </div>
          </div>
          <el-empty
            v-if="!loading && singleConversations.length === 0 && groupConversations.length === 0"
            :description="$t('暂无会话')"
          />
        </div>

        <div class="conv-pagination">
          <el-pagination
            layout="prev, pager, next"
            small
            background
            :total="pagination.total"
            :page-size="pagination.pageSize"
            :current-page="pagination.page"
            @current-change="handlePageChange"
          />
        </div>
      </el-card>

      <!-- 右：对话线程 -->
      <el-card
        class="thread-panel"
        shadow="never"
      >
        <template #header>
          <div
            v-if="currentConv"
            class="thread-header"
          >
            <div class="thread-title">
              <!-- M-2 客户头像链路：线程头部展示客户头像，空头像降级为默认人像图标 -->
              <el-avatar
                :size="32"
                :src="currentConv.sender_avatar || ''"
                class="thread-avatar"
              >
                <el-icon><User /></el-icon>
              </el-avatar>
              <span class="thread-name">{{ currentConv.customer_name || currentConv.customer_id || '-' }}</span>
              <el-tag
                size="small"
                :type="isGroupConv(currentConv) ? 'warning' : 'info'"
              >
                {{ isGroupConv(currentConv) ? $t('inboxT10.groupChatSession') : $t('inboxT10.singleChatSession') }}
              </el-tag>
              <el-tag
                size="small"
                :type="getChannelTagType(currentConv.platform) || 'info'"
              >
                {{ catalog.channelLabel(currentConv.platform) || currentConv.platform }}
              </el-tag>
              <el-tag
                v-if="currentConv.account_id"
                size="small"
                type="info"
                effect="plain"
              >
                {{ $t('inboxT10.account') }} {{ catalog.accountLabel(currentConv.account_id) }}
              </el-tag>
              <el-tag
                size="small"
                :type="statusTagType(currentConv.status)"
              >
                {{ statusLabel(currentConv.status) }}
              </el-tag>
            </div>
            <div class="thread-actions">
              <el-select
                v-if="isGroupConv(currentConv)"
                v-model="highlightSenderId"
                :placeholder="$t('inboxT10.highlightMember')"
                clearable
                filterable
                size="small"
                style="width: 150px"
              >
                <el-option
                  v-for="m in groupMembers"
                  :key="m.sender_id"
                  :label="m.sender_name || m.sender_id"
                  :value="m.sender_id"
                />
              </el-select>
              <el-tooltip :content="currentConv.pinned ? $t('取消置顶') : $t('置顶')">
                <el-button
                  size="small"
                  :type="currentConv.pinned ? 'primary' : 'default'"
                  :icon="Top"
                  circle
                  @click="handleTogglePin(currentConv)"
                />
              </el-tooltip>
              <el-tooltip :content="currentConv.starred ? $t('取消星标') : $t('星标')">
                <el-button
                  size="small"
                  :type="currentConv.starred ? 'warning' : 'default'"
                  :icon="currentConv.starred ? StarFilled : Star"
                  circle
                  @click="handleToggleStar(currentConv)"
                />
              </el-tooltip>
              <el-tooltip :content="currentConv.muted ? $t('取消静音') : $t('静音')">
                <el-button
                  size="small"
                  :type="currentConv.muted ? 'info' : 'default'"
                  :icon="Mute"
                  circle
                  @click="handleToggleMute(currentConv)"
                />
              </el-tooltip>
              <el-button
                size="small"
                @click="tagDialogVisible = true"
              >
                {{ $t('标签') }}
              </el-button>
              <el-dropdown
                trigger="click"
                @command="handleAssignCommand"
              >
                <el-button
                  size="small"
                  type="primary"
                >
                  {{ $t('分配') }}<el-icon class="el-icon--right">
                    <ArrowDown />
                  </el-icon>
                </el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item command="assign">
                      {{ $t('分配给坐席…') }}
                    </el-dropdown-item>
                    <el-dropdown-item command="auto-least">
                      {{ $t('自动分配（最少负载）') }}
                    </el-dropdown-item>
                    <el-dropdown-item command="auto-rr">
                      {{ $t('自动分配（轮询）') }}
                    </el-dropdown-item>
                    <el-dropdown-item
                      divided
                      command="release"
                    >
                      {{ $t('释放') }}
                    </el-dropdown-item>
                    <el-dropdown-item command="close">
                      {{ $t('关闭会话') }}
                    </el-dropdown-item>
                    <el-dropdown-item command="reopen">
                      {{ $t('重开会话') }}
                    </el-dropdown-item>
                    <el-dropdown-item
                      divided
                      command="history"
                    >
                      {{ $t('分配历史') }}
                    </el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </div>
          </div>
          <div
            v-else
            class="thread-header-empty"
          >
            {{ $t('选择左侧会话查看对话') }}
          </div>
        </template>

        <div
          v-if="currentConv"
          ref="threadBodyRef"
          v-loading="threadLoading"
          class="thread-body"
          :class="{ 'group-chat': isGroupConv(currentConv) }"
        >
          <div
            class="chat-mode-banner"
            :class="isGroupConv(currentConv) ? 'group' : 'single'"
          >
            <span>{{ isGroupConv(currentConv) ? $t('inboxT10.groupModeBanner') : $t('inboxT10.singleModeBanner') }}</span>
            <span
              v-if="currentConv.account_id"
              class="banner-account"
            >{{ $t('inboxT10.channelAccount') }}：{{ catalog.channelLabel(currentConv.platform) || currentConv.platform }} / {{ catalog.accountLabel(currentConv.account_id) }}</span>
          </div>
          <div
            v-for="msg in threadMessages"
            :key="`${msg.source}-${msg.id}`"
            class="msg-row"
            :class="{ mine: isPlatformMessage(msg), highlight: isHighlightedMember(msg, activeHighlight) }"
          >
            <!-- M-2 客户头像链路：入站消息展示发送者头像，空头像降级为默认人像图标 -->
            <el-avatar
              v-if="!isPlatformMessage(msg)"
              :size="28"
              :src="msg.sender_avatar || ''"
              class="msg-avatar"
            >
              <el-icon><User /></el-icon>
            </el-avatar>
            <div class="msg-bubble">
              <div class="msg-meta">
                <span class="msg-sender">{{ msg.sender_name || msg.sender_id || '-' }}</span>
                <el-tag
                  v-if="isHighlightedMember(msg, activeHighlight)"
                  size="small"
                  type="warning"
                  effect="dark"
                >
                  {{ $t('inboxT10.me') }}
                </el-tag>
                <el-tag
                  v-if="msg.is_ai_reply"
                  size="small"
                  type="success"
                  effect="plain"
                >
                  AI
                </el-tag>
                <el-tag
                  size="small"
                  :type="msg.source === 'hub' ? 'info' : 'warning'"
                  effect="plain"
                >
                  {{ msg.source === 'hub' ? $t('渠道') : $t('客服') }}
                </el-tag>
                <span class="msg-time">{{ formatTime(msg.sent_at || msg.created_at) }}</span>
                <el-popconfirm
                  :title="$t('确认删除这条消息？')"
                  @confirm="handleDeleteMessage(msg)"
                >
                  <template #reference>
                    <el-button
                      size="small"
                      type="danger"
                      link
                      :icon="Delete"
                      class="msg-del"
                    />
                  </template>
                </el-popconfirm>
              </div>
              <div class="msg-content">
                <el-image
                  v-if="isImage(msg)"
                  :src="msg.media_url"
                  :preview-src-list="[msg.media_url]"
                  fit="cover"
                  class="msg-image"
                  lazy
                >
                  <template #error>
                    <div class="img-error-fallback">
                      {{ $t('图片加载失败') }}
                    </div>
                  </template>
                </el-image>
                <!-- eslint-disable-next-line vuejs-accessibility/media-has-caption -- 客户上传的 UGC 音视频没有字幕轨，平台侧无法替上传者补 -->
                <video
                  v-else-if="isVideo(msg)"
                  :src="msg.media_url"
                  controls
                  preload="metadata"
                  class="msg-video"
                />
                <!-- eslint-disable-next-line vuejs-accessibility/media-has-caption -- 同上：UGC 语音消息无字幕源 -->
                <audio
                  v-else-if="isAudio(msg)"
                  :src="msg.media_url"
                  controls
                  preload="metadata"
                  class="msg-audio"
                />
                <div
                  v-else-if="isFile(msg)"
                  class="msg-file"
                >
                  <el-link
                    :href="safeMediaUrl(msg.media_url)"
                    target="_blank"
                    type="primary"
                    :underline="false"
                  >
                    <el-icon class="msg-file-icon">
                      <Document />
                    </el-icon>
                    {{ mediaFileName(msg.media_url) }}
                  </el-link>
                </div>
                <div
                  v-else-if="isLink(msg)"
                  class="msg-file"
                >
                  <el-link
                    :href="safeMediaUrl(msg.media_url)"
                    target="_blank"
                    type="primary"
                  >
                    {{ msg.content || msg.media_url }}
                  </el-link>
                </div>
                <template v-else-if="msg.content_type && msg.content_type !== 'text' && !hasMediaBody(msg)">
                  <el-tag
                    size="small"
                    effect="plain"
                    type="info"
                  >
                    {{ $t(typeLabel(msg)) }}
                  </el-tag>
                  <span v-if="msg.content && msg.content !== `[${typeLabel(msg)}]`">{{ msg.content }}</span>
                </template>
                <template v-else>
                  {{ msg.content || '-' }}
                </template>
              </div>
            </div>
          </div>
          <el-empty
            v-if="!threadLoading && threadMessages.length === 0"
            :description="$t('该会话暂无消息')"
          />
        </div>
        <el-empty
          v-else
          :description="$t('选择左侧会话查看对话')"
        />

        <!-- 人工回复（BUG-1，2026-11-04）：坐席在收件箱直接回复访客，
             走 POST /api/inbox/conversations/:id/reply（AI 会话自动接管为人工）。 -->
        <div
          v-if="currentConv"
          class="thread-reply"
        >
          <el-input
            v-model="replyDraft"
            type="textarea"
            :rows="2"
            maxlength="5000"
            show-word-limit
            :placeholder="$t('输入人工回复内容，Ctrl+Enter 发送')"
            @keydown.ctrl.enter.prevent="sendAgentReply"
            @keydown.meta.enter.prevent="sendAgentReply"
          />
          <div class="thread-reply-actions">
            <el-button
              size="small"
              @click="replyDraft = ''"
            >
              {{ $t('清空') }}
            </el-button>
            <el-button
              type="primary"
              size="small"
              :loading="replySending"
              :disabled="!replyDraft.trim()"
              @click="sendAgentReply"
            >
              {{ $t('回复访客') }}
            </el-button>
          </div>
        </div>
        <div
          v-if="currentConv"
          class="thread-pagination"
        >
          <el-pagination
            layout="prev, pager, next, total"
            small
            background
            :total="threadPagination.total"
            :page-size="threadPagination.pageSize"
            :current-page="threadPagination.page"
            @current-change="handleThreadPageChange"
          />
        </div>
      </el-card>
    </div>

    <!-- 分配给坐席对话框 -->
    <el-dialog
      v-model="assignDialogVisible"
      :title="$t('分配会话给坐席')"
      width="460px"
    >
      <el-form label-width="90px">
        <el-form-item :label="$t('坐席')">
          <el-select
            v-model="assignForm.to_user_id"
            filterable
            :placeholder="$t('选择坐席')"
            style="width: 100%"
          >
            <el-option
              v-for="s in staffOptions"
              :key="s.username"
              :label="`${s.real_name || s.username}`"
              :value="s.username"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('备注')">
          <el-input
            v-model="assignForm.remark"
            type="textarea"
            :rows="2"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="assignDialogVisible = false">
          {{ $t('取消') }}
        </el-button>
        <el-button
          type="primary"
          :loading="assignSubmitting"
          @click="submitAssign"
        >
          {{ $t('确定') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 标签管理对话框 -->
    <el-dialog
      v-model="tagDialogVisible"
      :title="$t('会话标签')"
      width="420px"
    >
      <div
        v-if="currentConv"
        class="tag-list"
      >
        <el-tag
          v-for="tag in (currentConv.tags || [])"
          :key="tag"
          closable
          type="warning"
          effect="plain"
          style="margin: 0 8px 8px 0"
          @close="handleRemoveTag(currentConv, tag)"
        >
          {{ tag }}
        </el-tag>
        <span
          v-if="!(currentConv.tags || []).length"
          class="tag-empty"
        >{{ $t('暂无标签') }}</span>
      </div>
      <el-input
        v-model="newTag"
        :placeholder="$t('输入标签后回车添加')"
        clearable
        @keyup.enter="handleAddTag"
      >
        <template #append>
          <el-button @click="handleAddTag">
            {{ $t('添加') }}
          </el-button>
        </template>
      </el-input>
      <template #footer>
        <el-button @click="tagDialogVisible = false">
          {{ $t('关闭') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 分配历史对话框 -->
    <el-dialog
      v-model="historyDialogVisible"
      :title="$t('分配历史')"
      width="640px"
    >
      <el-table
        v-loading="historyLoading"
        :data="assignmentHistory"
        border
        size="small"
      >
        <el-table-column
          prop="id"
          label="ID"
          width="60"
        />
        <el-table-column
          prop="action"
          :label="$t('动作')"
          width="90"
        >
          <template #default="{ row }">
            {{ actionLabel(row.action) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="to_type"
          :label="$t('对象类型')"
          width="90"
        >
          <template #default="{ row }">
            {{ toTypeLabel(row.to_type) }}
          </template>
        </el-table-column>
        <el-table-column
          prop="to_user_id"
          :label="$t('坐席/SOP')"
          min-width="110"
        >
          <template #default="{ row }">
            <EntityLabel
              v-if="row.to_user_id"
              entity="user"
              :value="row.to_user_id"
            />
            <span v-else-if="row.to_sop_id">SOP#{{ row.to_sop_id }}</span>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column
          prop="operator_id"
          :label="$t('操作人')"
          min-width="130"
        >
          <template #default="{ row }">
            <EntityLabel
              entity="user"
              :value="row.operator_id"
            />
          </template>
        </el-table-column>
        <el-table-column
          prop="created_at"
          :label="$t('时间')"
          min-width="150"
        >
          <template #default="{ row }">
            {{ formatTime(row.created_at) }}
          </template>
        </el-table-column>
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import {
  Search, RefreshRight, Refresh, DataAnalysis, Top, Star, StarFilled, Mute,
  Delete, ArrowDown, Document, User
} from '@element-plus/icons-vue'
import { inboxApi } from '@/api/inbox'
import { http } from '@/utils/request'
import { useUserStore } from '@/stores/user'
import { getChannelTagType } from '@/constants/channel'
import ChannelSelect from '@/components/ChannelSelect.vue'
import EntityLabel from '@/components/EntityLabel.vue'
import { useChannelCatalogStore } from '@/stores/channelCatalog'
import {
  isPlatformMessage, isGroupConversation, partitionInboxConversations,
  isHighlightedMember, defaultMemberHighlight, buildInboxListParams
} from '@/utils/inboxView'
import i18n from '@/i18n'

const t = (k) => i18n.global.t(k)

const loading = ref(false)
const threadLoading = ref(false)
const conversations = ref([])
const groupCards = ref([])
const currentConv = ref(null)
const currentGroup = ref(null)
const groupMembers = ref([])
const highlightSenderId = ref('')
const threadMessages = ref([])
const threadBodyRef = ref(null)
const stats = ref(null)

const searchForm = reactive({
  platform: '',
  account_id: '',
  conversation_type: '',
  status: '',
  filter: '',
  pinned: '',
  nickname: '',
  keyword: ''
})

const pagination = reactive({ page: 1, pageSize: 20, total: 0 })
const threadPagination = reactive({ page: 1, pageSize: 50, total: 0 })

const assignDialogVisible = ref(false)
const assignSubmitting = ref(false)
const assignForm = reactive({ to_user_id: '', remark: '' })
const staffOptions = ref([])

// 人工回复（BUG-1，2026-11-04）：落 POST /api/inbox/conversations/:id/reply。
// 成功后把返回的 message 追加进当前线程底部，不等下一次轮询。
const replyDraft = ref('')
const replySending = ref(false)
const sendAgentReply = async () => {
  if (!currentConv.value || replySending.value) return
  const content = (replyDraft.value || '').trim()
  if (!content) return
  replySending.value = true
  try {
    const res = await http.post(
      `/api/inbox/conversations/${currentConv.value.id}/reply`,
      { content }
    )
    const delivery = res?.delivery || res?.data?.delivery
    const msg = res?.data?.message || res?.message
    if (msg && msg.id) {
      threadMessages.value = [...threadMessages.value, msg]
      threadPagination.total += 1
    }
    // 后端按真实投递状态返回文案：失败/无路由不显示"已发送"，保留草稿供重试。
    const status = delivery?.delivery_status || ''
    const serverText = res?.data?.message || res?.message || ''
    if (status === 'failed' || status === 'no_route') {
      ElMessage.error(serverText || t('回复未送达'))
      return
    }
    if (status === 'platform_confirmed' || status === 'accepted') {
      ElMessage.warning(serverText || t('回复已发送'))
    } else {
      ElMessage.success(serverText || t('回复已发送'))
    }
    replyDraft.value = ''
  } catch {
    // request.js 已统一弹错，这里不再重复提示
  } finally {
    replySending.value = false
  }
}

const tagDialogVisible = ref(false)
const newTag = ref('')

const historyDialogVisible = ref(false)
const historyLoading = ref(false)
const assignmentHistory = ref([])

const userStore = useUserStore()
const catalog = useChannelCatalogStore()
let refreshTimer = null

// ---- T10 展示态：单聊/群聊分区、群成员高亮（均为只读投影）----
const accountOptions = computed(() => catalog.accountOptions(searchForm.platform))
const singleConversations = computed(() => conversations.value.filter((c) => !isGroupConversation(c)))
const groupConversations = computed(() => groupCards.value)
const activeHighlight = computed(() => {
  const id = String(highlightSenderId.value || '')
  if (!id) return null
  const member = groupMembers.value.find((m) => String(m.sender_id || '') === id)
  return member ? { sender_id: member.sender_id, sender_name: member.sender_name } : { sender_id: id, sender_name: '' }
})
// 渠道切换后账号选项变化，清掉不再可选的账号，避免筛出空结果。
watch(() => searchForm.platform, () => { searchForm.account_id = '' })

// ---- 字典 ----
const STATUS_MAP = {
  unread: { label: '未读', type: 'danger' },
  open: { label: '待处理', type: 'warning' },
  assigned: { label: '已分配', type: 'primary' },
  closed: { label: '已关闭', type: 'success' },
  snoozed: { label: '稍后处理', type: 'info' }
}
// 非字典值（历史脏数据）归一到未读/待处理显示，避免出现原始英文状态
const statusLabel = (v) => {
  if (STATUS_MAP[v]) return t(STATUS_MAP[v].label)
  if (!v) return '-'
  return t((v.unread_count ?? 0) > 0 ? '未读' : '待处理')
}
const statusTagType = (v) => (STATUS_MAP[v] ? STATUS_MAP[v].type : 'danger')

const ACTION_MAP = {
  assign: '分配', reassign: '改派', release: '释放', close: '关闭', reopen: '重开'
}
const actionLabel = (v) => (ACTION_MAP[v] ? t(ACTION_MAP[v]) : (v || '-'))

const TOTYPE_MAP = { human: '人工', sop: 'SOP', ai: 'AI' }
const toTypeLabel = (v) => (TOTYPE_MAP[v] ? t(TOTYPE_MAP[v]) : (v || '-'))

const isImage = (msg) =>
  (msg.content_type === 'image' || /\.(png|jpe?g|gif|webp)(\?|$)/i.test(msg.media_url || '')) && !!msg.media_url

// ---- 多类型消息渲染判定（002-multi-type-messages）----
// content_type 为中台词表（text/image/video/audio/file/link/card/location）；
// 兜底用 media_url 扩展名嗅探（直连渠道转存后的 URL 带扩展名）。
const extOf = (url) => {
  const m = /\.(mp4|mov|m4v|webm|mkv|mp3|wav|m4a|aac|ogg|amr|pdf|docx?|xlsx?|pptx?|zip|rar|7z|txt|csv)(\?|$)/i.exec(url || '')
  return m ? m[1].toLowerCase() : ''
}
const VIDEO_EXTS = ['mp4', 'mov', 'm4v', 'webm', 'mkv']
const AUDIO_EXTS = ['mp3', 'wav', 'm4a', 'aac', 'ogg', 'amr']

const isVideo = (msg) => {
  if (!msg.media_url) return false
  if (msg.content_type === 'video') return true
  if (msg.content_type && msg.content_type !== 'text') return false
  return VIDEO_EXTS.includes(extOf(msg.media_url))
}
const isAudio = (msg) => {
  if (!msg.media_url) return false
  if (msg.content_type === 'audio') return true
  if (msg.content_type && msg.content_type !== 'text') return false
  return AUDIO_EXTS.includes(extOf(msg.media_url))
}
const isFile = (msg) => {
  if (!msg.media_url) return false
  if (msg.content_type === 'file') return true
  if (msg.content_type && msg.content_type !== 'text') return false
  const ext = extOf(msg.media_url)
  return !!ext && !VIDEO_EXTS.includes(ext) && !AUDIO_EXTS.includes(ext) &&
    !/\.(png|jpe?g|gif|webp)(\?|$)/i.test(msg.media_url)
}
const isLink = (msg) => msg.content_type === 'link' && !!msg.media_url
// 非文本且无可用 media_url：显示占位标签（如「[语音]」）而不是一行空文本。
// 值是词条键（中文整键，语言包 4a60d3af 风格），模板 $t(typeLabel(msg)) 消费；
// collect_literal_key_refs 的整键扫描认这一形态（QUOTED 字面量 == 词条 leaf）。
const MSG_TYPE_LABELS = { image: '图片', video: '视频', audio: '语音', file: '文件', link: '链接', card: '卡片', location: '位置' }
const typeLabel = (msg) => MSG_TYPE_LABELS[msg.content_type] || msg.content_type || '消息'
const hasMediaBody = (msg) => !!msg.media_url && (isImage(msg) || isVideo(msg) || isAudio(msg) || isFile(msg) || isLink(msg))
// 媒体/链接 href 协议白名单（2026-11-15 审计 M-2）：media_url 上游含外部人可控数据，
// javascript:/data: 伪协议直进 el-link href 是注入点；与 RichCard.vue 的 safeUrl 同口径
const safeMediaUrl = (url) => {
  if (!url || typeof url !== 'string') return undefined
  try {
    const u = new URL(url, window.location.origin)
    return ['http:', 'https:'].includes(u.protocol) ? url : undefined
  } catch {
    return undefined
  }
}
const mediaFileName = (url) => {
  try { return decodeURIComponent((url || '').split('?')[0].split('/').pop() || '文件') } catch { return '文件' }
}

// T10：自他/群类型以服务端投影为准；本地只做保守回退（见 utils/inboxView）。
const isGroupConv = (conv) => isGroupConversation(conv)

const formatTime = (v) => {
  if (!v) return '-'
  const d = new Date(v)
  if (isNaN(d.getTime())) return String(v)
  const now = new Date()
  const sameDay = d.toDateString() === now.toDateString()
  const hm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  return sameDay ? hm : `${d.getMonth() + 1}/${d.getDate()} ${hm}`
}

// 等待时长（specs/024 T606）：分钟/小时/天三档足够，列表里不需要秒级精度。
const formatDuration = (v) => {
  const ms = Date.now() - new Date(v).getTime()
  if (!isFinite(ms) || ms < 0) return '0m'
  const mins = Math.floor(ms / 60000)
  if (mins < 60) return `${mins}m`
  const hours = Math.floor(mins / 60)
  if (hours < 24) return `${hours}h${mins % 60}m`
  return `${Math.floor(hours / 24)}d`
}

// 待首响徽标：客户在等（waiting_since 有值）、我们还没回（first_reply_at 为空），
// 且会话不在 closed/snoozed——snoozed 是「我稍后再看」，不是「客户在等」。
const waitingBadge = (conv) => {
  if (!conv || !conv.waiting_since || conv.first_reply_at) return ''
  if (conv.status === 'closed' || conv.status === 'snoozed') return ''
  return `${t('等待')} ${formatDuration(conv.waiting_since)}`
}

// ---- 数据加载 ----
const fetchConversations = async () => {
  loading.value = true
  try {
    const params = buildInboxListParams(searchForm, pagination.page, pagination.pageSize)
    const res = await inboxApi.listConversations(params)
    const data = res?.data || res || {}
    conversations.value = data.list || []
    // 群聊按 group_key 只读折叠成一张卡；单聊原样保留各自绑定。
    groupCards.value = partitionInboxConversations(conversations.value).groups
    pagination.total = data.total || 0
  } catch (e) {
    console.error('[inbox] fetchConversations', e)
  } finally {
    loading.value = false
  }
}

const loadStats = async () => {
  try {
    const res = await inboxApi.getStats()
    stats.value = res?.data || res || null
  } catch (e) {
    console.error('[inbox] loadStats', e)
  }
}

const fetchThread = async () => {
  if (!currentConv.value) return
  threadLoading.value = true
  try {
    let data
    if (currentGroup.value) {
      // 群聊：一次拉全群合并线程，成员下拉来自同一响应，避免逐会话 N+1。
      const res = await inboxApi.getGroupThread({
        platform: currentGroup.value.platform,
        account_id: currentGroup.value.account_id,
        group_name: currentGroup.value.group_name,
        page: threadPagination.page,
        page_size: threadPagination.pageSize
      })
      data = res?.data || res || {}
      groupMembers.value = data.members || []
      if (!highlightSenderId.value && groupMembers.value.length) {
        const def = defaultMemberHighlight(currentGroup.value, groupMembers.value)
        if (def && def.sender_id) highlightSenderId.value = def.sender_id
      }
    } else {
      const res = await inboxApi.getMessages(currentConv.value.id, {
        page: threadPagination.page,
        page_size: threadPagination.pageSize
      })
      data = res?.data || res || {}
    }
    // 后端按时间倒序返回，前端展示为正序对话
    threadMessages.value = (data.list || []).slice().reverse()
    threadPagination.total = data.total || 0
    await nextTick()
    scrollThreadToBottom()
  } catch (e) {
    console.error('[inbox] fetchThread', e)
  } finally {
    threadLoading.value = false
  }
}

const scrollThreadToBottom = () => {
  const el = threadBodyRef.value
  if (el?.$el) el.$el.scrollTop = el.$el.scrollHeight
  else if (el) el.scrollTop = el.scrollHeight
}

const selectConversation = async (conv) => {
  currentConv.value = conv
  currentGroup.value = null
  groupMembers.value = []
  highlightSenderId.value = ''
  threadPagination.page = 1
  threadPagination.total = 0
  threadMessages.value = []
  fetchThread()
  // 有未读则顺带标记已读（历史脏状态如 'active' 一并按有未读处理）
  const isUnreadLike = conv.status === 'unread' || !['open', 'assigned', 'closed'].includes(conv.status)
  if (conv.unread_count > 0 || isUnreadLike) {
    try {
      await inboxApi.markRead(conv.id)
      conv.unread_count = 0
      if (isUnreadLike) conv.status = 'open'
    } catch (e) { console.warn('[inbox] markRead', e) }
  }
}

// 群卡是「同一群所有成员发言」的入口；默认高亮当前卡绑定的 CRM 人（customer_id），
// 群成员下拉可改选其他人。群卡仍用代表会话 id 做已读/回复，客户绑定不变。
const selectGroupCard = async (card) => {
  currentConv.value = card
  currentGroup.value = card
  groupMembers.value = []
  highlightSenderId.value = String(card.customer_id || card.sender_id || '')
  threadPagination.page = 1
  threadPagination.total = 0
  threadMessages.value = []
  fetchThread()
  const isUnreadLike = card.status === 'unread' || !['open', 'assigned', 'closed'].includes(card.status)
  if (card.unread_count > 0 || isUnreadLike) {
    try {
      await inboxApi.markRead(card.id)
      card.unread_count = 0
      if (isUnreadLike) card.status = 'open'
    } catch (e) { console.warn('[inbox] markRead group', e) }
  }
}

const refreshAll = () => {
  fetchConversations()
  loadStats()
  if (currentConv.value) fetchThread()
}

const handleSearch = () => {
  pagination.page = 1
  fetchConversations()
}

const handlePageChange = (p) => {
  pagination.page = p
  fetchConversations()
}

const handleThreadPageChange = (p) => {
  threadPagination.page = p
  fetchThread()
}

// ---- 会话操作 ----
const refreshCurrentInPlace = async () => {
  if (!currentConv.value) return
  try {
    const res = await inboxApi.getConversation(currentConv.value.id)
    const fresh = res?.data || res
    if (fresh && fresh.id) {
      const idx = conversations.value.findIndex((c) => c.id === fresh.id)
      if (idx >= 0) conversations.value.splice(idx, 1, fresh)
      currentConv.value = fresh
    }
  } catch (e) { console.warn('[inbox] refreshCurrent', e) }
}

const handleTogglePin = async (conv) => {
  try {
    await inboxApi.setPin(conv.id, !conv.pinned)
    await refreshCurrentInPlace()
  } catch (e) { console.error('[inbox] pin', e) }
}

const handleToggleStar = async (conv) => {
  try {
    await inboxApi.setStar(conv.id, !conv.starred)
    await refreshCurrentInPlace()
  } catch (e) { console.error('[inbox] star', e) }
}

const handleToggleMute = async (conv) => {
  try {
    await inboxApi.setMute(conv.id, !conv.muted)
    await refreshCurrentInPlace()
  } catch (e) { console.error('[inbox] mute', e) }
}

const handleAddTag = async () => {
  const tag = (newTag.value || '').trim()
  if (!tag || !currentConv.value) return
  try {
    await inboxApi.addTag(currentConv.value.id, tag)
    newTag.value = ''
    await refreshCurrentInPlace()
  } catch (e) { console.error('[inbox] addTag', e) }
}

const handleRemoveTag = async (conv, tag) => {
  try {
    await inboxApi.removeTag(conv.id, tag)
    await refreshCurrentInPlace()
  } catch (e) { console.error('[inbox] removeTag', e) }
}

const handleDeleteMessage = async (msg) => {
  if (!currentConv.value) return
  try {
    await inboxApi.deleteMessage(currentConv.value.id, msg.id, msg.source || 'hub')
    ElMessage.success(t('删除成功'))
    fetchThread()
  } catch (e) { console.error('[inbox] deleteMessage', e) }
}

// ---- 分配 ----
const loadStaffOptions = async () => {
  if (staffOptions.value.length) return
  try {
    const res = await inboxApi.listAgents()
    const data = res?.data || res || {}
    staffOptions.value = data.list || []
  } catch (e) {
    console.warn('[inbox] loadStaff', e)
    staffOptions.value = []
  }
}

const handleAssignCommand = async (cmd) => {
  if (!currentConv.value) return
  const conv = currentConv.value
  if (cmd === 'assign') {
    assignForm.to_user_id = ''
    assignForm.remark = ''
    await loadStaffOptions()
    assignDialogVisible.value = true
    return
  }
  if (cmd === 'auto-least' || cmd === 'auto-rr') {
    await loadStaffOptions()
    const candidates = staffOptions.value.map((s) => s.username).filter(Boolean)
    if (!candidates.length) {
      ElMessage.warning(t('暂无可分配坐席'))
      return
    }
    try {
      await inboxApi.autoAssign({
        conversation_id: conv.id,
        candidates,
        mode: cmd === 'auto-rr' ? 'round_robin' : 'least_busy'
      })
      ElMessage.success(t('分配成功'))
      refreshAll()
    } catch (e) { console.error('[inbox] autoAssign', e) }
    return
  }
  if (cmd === 'history') {
    historyDialogVisible.value = true
    historyLoading.value = true
    try {
      const res = await inboxApi.listAssignments({ conversation_id: conv.id, page: 1, page_size: 50 })
      const data = res?.data || res || {}
      assignmentHistory.value = data.list || []
    } catch (e) {
      console.error('[inbox] assignments', e)
      assignmentHistory.value = []
    } finally {
      historyLoading.value = false
    }
    return
  }
  // release / close / reopen
  try {
    await inboxApi.assign({
      conversation_id: conv.id,
      action: cmd
    })
    ElMessage.success(t('操作成功'))
    refreshAll()
  } catch (e) { console.error('[inbox] assign', cmd, e) }
}

const submitAssign = async () => {
  if (!assignForm.to_user_id) {
    ElMessage.warning(t('请选择坐席'))
    return
  }
  assignSubmitting.value = true
  try {
    await inboxApi.assign({
      conversation_id: currentConv.value.id,
      action: currentConv.value.assigned_to ? 'reassign' : 'assign',
      to_type: 'human',
      to_user_id: assignForm.to_user_id,
      remark: assignForm.remark
    })
    ElMessage.success(t('分配成功'))
    assignDialogVisible.value = false
    refreshAll()
  } catch (e) {
    console.error('[inbox] submitAssign', e)
  } finally {
    assignSubmitting.value = false
  }
}

// ---- 对账 ----
const handleReconcile = async (mode) => {
  try {
    const res = await inboxApi.reconcile(mode)
    const data = res?.data || res || {}
    ElMessage.success(data.message || t('对账完成'))
    refreshAll()
  } catch (e) { console.error('[inbox] reconcile', e) }
}

// ---- 轮询刷新（30s，保持未读/新会话感知） ----
const startPolling = () => {
  stopPolling()
  refreshTimer = setInterval(() => {
    fetchConversations()
    loadStats()
  }, 30000)
}
const stopPolling = () => {
  if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null }
}

onMounted(() => {
  catalog.ensure()
  refreshAll()
  startPolling()
})

onUnmounted(stopPolling)
</script>

<style scoped>
.inbox-page {
  padding: 0;
}
.header-card {
  margin-bottom: 12px;
}
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
}
.page-header h2 {
  margin: 0 0 4px;
}
.subtitle {
  margin: 0;
  color: #909399;
  font-size: 13px;
}
.stats-row {
  margin-top: 10px;
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.workbench {
  display: flex;
  gap: 12px;
  align-items: stretch;
}
.conv-panel {
  width: 420px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
}
.thread-panel {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.conv-filters {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}
.conv-list {
  min-height: 300px;
  max-height: calc(100vh - 340px);
  overflow-y: auto;
}
/* T10 单聊/群聊分区标题：只读组织，不改任何会话绑定 */
.conv-section-title {
  padding: 6px 10px;
  margin: 4px 0 6px;
  color: var(--el-text-color-secondary);
  background: var(--el-fill-color-lighter);
  border-radius: 4px;
  font-size: 12px;
  font-weight: 600;
}
.conv-group-item .conv-avatar {
  background: var(--el-color-warning-light-9);
}
.conv-item {
  padding: 10px 12px;
  border-bottom: 1px solid var(--el-border-color-lighter);
  cursor: pointer;
  border-radius: 6px;
  transition: background-color 0.15s;
}
.conv-item:hover {
  background-color: var(--el-fill-color-light);
}
.conv-item.active {
  background-color: var(--el-color-primary-light-9);
}
.conv-item-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.conv-name {
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
/* M-2 客户头像链路：会话列表头像（空 src 时 el-avatar 显示默认人像插槽） */
.conv-avatar {
  flex-shrink: 0;
  background: var(--el-fill-color);
  color: var(--el-text-color-secondary);
}
.pin-icon { color: var(--el-color-primary); }
.mute-icon { color: var(--el-color-info); }
.conv-time {
  color: #909399;
  font-size: 12px;
  flex-shrink: 0;
}
.conv-item-mid {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 4px;
}
.conv-preview {
  color: #606266;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}
.conv-badge { flex-shrink: 0; margin-left: 8px; }
.conv-item-bottom {
  display: flex;
  gap: 6px;
  align-items: center;
  margin-top: 6px;
  flex-wrap: wrap;
}
.star-icon { color: var(--el-color-warning); }
.conv-pagination {
  padding-top: 10px;
  display: flex;
  justify-content: center;
}
.thread-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.thread-header-empty {
  color: #909399;
  text-align: center;
  width: 100%;
}
.thread-title {
  display: flex;
  align-items: center;
  gap: 8px;
}
/* M-2 客户头像链路：线程头部头像（空 src 时 el-avatar 显示默认人像插槽） */
.thread-avatar {
  flex-shrink: 0;
  background: var(--el-fill-color);
  color: var(--el-text-color-secondary);
}
.thread-name {
  font-weight: 600;
  font-size: 15px;
}
.thread-actions {
  display: flex;
  align-items: center;
  gap: 6px;
}
.thread-body {
  min-height: 300px;
  max-height: calc(100vh - 380px);
  overflow-y: auto;
  padding: 8px 4px;
}
.thread-body.group-chat { background: #fafcff; }
.chat-mode-banner { display:flex; justify-content:space-between; gap:8px; padding:8px 10px; margin-bottom:10px; border-radius:6px; font-size:12px; }
.chat-mode-banner.single { background:#f4f6f8; color:#606266; }
.chat-mode-banner.group { background:#fff7e6; color:#b26a00; }
.banner-account { color:#909399; }
.msg-row {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  margin-bottom: 12px;
}
/* M-2 客户头像链路：入站消息头像（空 src 时 el-avatar 显示默认人像插槽） */
.msg-avatar {
  flex-shrink: 0;
  background: var(--el-fill-color);
  color: var(--el-text-color-secondary);
}
.msg-row.mine {
  justify-content: flex-end;
}
.msg-row.mine .msg-avatar {
  display: none;
}
.msg-bubble {
  max-width: 72%;
  background: var(--el-fill-color-light);
  border-radius: 8px;
  padding: 8px 12px;
}
.msg-row.mine .msg-bubble {
  background: var(--el-color-primary-light-9);
}
/* T10 群聊当前成员高亮：仅视觉标记，不影响左右分布（平台发言仍在右） */
.msg-row.highlight .msg-bubble {
  border-left: 3px solid var(--el-color-warning);
  background: var(--el-color-warning-light-9);
}
.msg-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #909399;
  margin-bottom: 4px;
  flex-wrap: wrap;
}
.msg-sender {
  font-weight: 600;
  color: #606266;
}
.msg-time { flex-shrink: 0; }
.msg-del { margin-left: auto; }
.msg-content {
  font-size: 14px;
  line-height: 1.5;
  word-break: break-word;
  white-space: pre-wrap;
}
.msg-image {
  max-width: 220px;
  max-height: 180px;
  border-radius: 4px;
}
/* 002-multi-type-messages：视频/音频/文件卡片 */
.msg-video {
  max-width: 280px;
  max-height: 200px;
  border-radius: 4px;
  display: block;
}
.msg-audio {
  max-width: 260px;
  display: block;
}
.msg-file {
  display: flex;
  align-items: center;
  gap: 4px;
  max-width: 280px;
  word-break: break-all;
}
.msg-file-icon {
  margin-right: 2px;
}
.thread-reply {
  padding-top: 10px;
  border-top: 1px solid #ebeef5;
  margin-top: 8px;
}
.thread-reply-actions {
  margin-top: 8px;
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.thread-pagination {
  padding-top: 10px;
  display: flex;
  justify-content: center;
}
.tag-empty { color: #909399; font-size: 13px; }
.tag-list { margin-bottom: 12px; }
</style>
