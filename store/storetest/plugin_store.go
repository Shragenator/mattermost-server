// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package storetest

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/mattermost/mattermost-server/v5/store"
	"github.com/stretchr/testify/assert"
)

func TestPluginStore(t *testing.T, ss store.Store, s SqlSupplier) {
	t.Run("SaveOrUpdate", func(t *testing.T) { testPluginSaveOrUpdate(t, ss, s) })
	t.Run("CompareAndSet", func(t *testing.T) { testPluginCompareAndSet(t, ss) })
	t.Run("PluginSaveGet", func(t *testing.T) { testPluginSaveGet(t, ss) })
	t.Run("PluginSaveGetExpiry", func(t *testing.T) { testPluginSaveGetExpiry(t, ss) })
	t.Run("PluginDelete", func(t *testing.T) { testPluginDelete(t, ss) })
	t.Run("PluginDeleteAll", func(t *testing.T) { testPluginDeleteAll(t, ss) })
	t.Run("PluginDeleteExpired", func(t *testing.T) { testPluginDeleteExpired(t, ss) })
}

func testPluginSaveOrUpdate(t *testing.T, ss store.Store, s SqlSupplier) {
	t.Run("invalid kv", func(t *testing.T) {
		kv := &model.PluginKeyValue{
			PluginId: "",
			Key:      model.NewId(),
			Value:    []byte(model.NewId()),
			ExpireAt: 0,
		}

		kv, err := ss.Plugin().SaveOrUpdate(kv)
		require.NotNil(t, err)
		require.Equal(t, "model.plugin_key_value.is_valid.plugin_id.app_error", err.Id)
		assert.Nil(t, kv)
	})

	t.Run("new key", func(t *testing.T) {
		pluginId := model.NewId()
		key := model.NewId()
		value := model.NewId()
		expireAt := int64(0)

		kv := &model.PluginKeyValue{
			PluginId: pluginId,
			Key:      key,
			Value:    []byte(value),
			ExpireAt: expireAt,
		}

		retKv, err := ss.Plugin().SaveOrUpdate(kv)
		require.Nil(t, err)
		assert.Equal(t, kv, retKv)
		assert.Equal(t, pluginId, kv.PluginId)
		assert.Equal(t, key, kv.Key)
		assert.Equal(t, []byte(value), retKv.Value)
		assert.Equal(t, expireAt, kv.ExpireAt)
	})

	t.Run("existing key", func(t *testing.T) {
		pluginId := model.NewId()
		key := model.NewId()
		value := model.NewId()
		expireAt := int64(0)

		kv := &model.PluginKeyValue{
			PluginId: pluginId,
			Key:      key,
			Value:    []byte(value),
			ExpireAt: expireAt,
		}

		_, err := ss.Plugin().SaveOrUpdate(kv)
		require.Nil(t, err)

		newValue := model.NewId()
		kv.Value = []byte(newValue)
		retKv, err := ss.Plugin().SaveOrUpdate(kv)

		require.Nil(t, err)
		assert.Equal(t, kv, retKv)
		assert.Equal(t, pluginId, kv.PluginId)
		assert.Equal(t, key, kv.Key)
		assert.Equal(t, []byte(newValue), retKv.Value)
		assert.Equal(t, expireAt, kv.ExpireAt)
	})

	t.Run("racey inserts", func(t *testing.T) {
		pluginId := model.NewId()
		key := model.NewId()
		value := model.NewId()
		expireAt := int64(0)

		kv := &model.PluginKeyValue{
			PluginId: pluginId,
			Key:      key,
			Value:    []byte(value),
			ExpireAt: expireAt,
		}

		var wg sync.WaitGroup
		var count int32
		done := make(chan bool)

		inserter := func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					_, err := ss.Plugin().SaveOrUpdate(kv)
					require.Nil(t, err)
					atomic.AddInt32(&count, 1)
				}
			}
		}

		deleter := func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					_, err := s.GetMaster().Exec("DELETE FROM PluginKeyValueStore")
					require.NoError(t, err)
				}
			}
		}

		wg.Add(1)
		go inserter()
		wg.Add(1)
		go inserter()
		wg.Add(1)
		go inserter()
		wg.Add(1)
		go deleter()

		require.Eventually(t, func() bool {
			return atomic.LoadInt32(&count) >= 1000
		}, 5*time.Second, 100*time.Millisecond)

		close(done)
		wg.Wait()
	})
}

func testPluginCompareAndSet(t *testing.T, ss store.Store) {
	kv := &model.PluginKeyValue{
		PluginId: model.NewId(),
		Key:      model.NewId(),
		Value:    []byte(model.NewId()),
		ExpireAt: 0,
	}
	defer func() {
		_ = ss.Plugin().Delete(kv.PluginId, kv.Key)
	}()

	t.Run("set non-existent key should succeed given nil old value", func(t *testing.T) {
		ok, err := ss.Plugin().CompareAndSet(kv, nil)
		require.Nil(t, err)
		assert.True(t, ok)
	})

	t.Run("set existing key without old value should fail without error because is a automatically handled race condition", func(t *testing.T) {
		_, err := ss.Plugin().SaveOrUpdate(kv)
		require.Nil(t, err)

		kvNew := &model.PluginKeyValue{
			PluginId: kv.PluginId,
			Key:      kv.Key,
			Value:    []byte(model.NewId()),
			ExpireAt: 0,
		}

		ok, err := ss.Plugin().CompareAndSet(kvNew, nil)
		require.Nil(t, err)
		assert.False(t, ok)
	})

	t.Run("set existing key with new value should succeed given same old value", func(t *testing.T) {
		_, err := ss.Plugin().SaveOrUpdate(kv)
		require.Nil(t, err)

		kvNew := &model.PluginKeyValue{
			PluginId: kv.PluginId,
			Key:      kv.Key,
			Value:    []byte(model.NewId()),
			ExpireAt: 0,
		}

		ok, err := ss.Plugin().CompareAndSet(kvNew, kv.Value)
		require.Nil(t, err)
		assert.True(t, ok)
	})

	t.Run("set existing key with new value should fail given different old value", func(t *testing.T) {
		_, err := ss.Plugin().SaveOrUpdate(kv)
		require.Nil(t, err)

		kvNew := &model.PluginKeyValue{
			PluginId: kv.PluginId,
			Key:      kv.Key,
			Value:    []byte(model.NewId()),
			ExpireAt: 0,
		}

		ok, err := ss.Plugin().CompareAndSet(kvNew, []byte(model.NewId()))
		require.Nil(t, err)
		assert.False(t, ok)
	})

	t.Run("set existing key with same value should succeed given same old value", func(t *testing.T) {
		_, err := ss.Plugin().SaveOrUpdate(kv)
		require.Nil(t, err)

		ok, err := ss.Plugin().CompareAndSet(kv, kv.Value)
		require.Nil(t, err)
		assert.True(t, ok)
	})

	t.Run("set existing key with same value should fail given different old value", func(t *testing.T) {
		_, err := ss.Plugin().SaveOrUpdate(kv)
		require.Nil(t, err)

		ok, err := ss.Plugin().CompareAndSet(kv, []byte(model.NewId()))
		require.Nil(t, err)
		assert.False(t, ok)
	})
}

func testPluginSaveGet(t *testing.T, ss store.Store) {
	kv := &model.PluginKeyValue{
		PluginId: model.NewId(),
		Key:      model.NewId(),
		Value:    []byte(model.NewId()),
		ExpireAt: 0,
	}

	_, err := ss.Plugin().SaveOrUpdate(kv)
	require.Nil(t, err)

	defer func() {
		_ = ss.Plugin().Delete(kv.PluginId, kv.Key)
	}()

	received, err := ss.Plugin().Get(kv.PluginId, kv.Key)
	require.Nil(t, err)
	assert.Equal(t, kv.PluginId, received.PluginId)
	assert.Equal(t, kv.Key, received.Key)
	assert.Equal(t, kv.Value, received.Value)
	assert.Equal(t, kv.ExpireAt, received.ExpireAt)

	// Try inserting when already exists
	kv.Value = []byte(model.NewId())
	_, err = ss.Plugin().SaveOrUpdate(kv)
	require.Nil(t, err)

	received, err = ss.Plugin().Get(kv.PluginId, kv.Key)
	require.Nil(t, err)
	assert.Equal(t, kv.PluginId, received.PluginId)
	assert.Equal(t, kv.Key, received.Key)
	assert.Equal(t, kv.Value, received.Value)
}

func testPluginSaveGetExpiry(t *testing.T, ss store.Store) {
	kv := &model.PluginKeyValue{
		PluginId: model.NewId(),
		Key:      model.NewId(),
		Value:    []byte(model.NewId()),
		ExpireAt: model.GetMillis() + 30000,
	}

	_, err := ss.Plugin().SaveOrUpdate(kv)
	require.Nil(t, err)

	defer func() {
		_ = ss.Plugin().Delete(kv.PluginId, kv.Key)
	}()

	received, err := ss.Plugin().Get(kv.PluginId, kv.Key)
	require.Nil(t, err)
	assert.Equal(t, kv.PluginId, received.PluginId)
	assert.Equal(t, kv.Key, received.Key)
	assert.Equal(t, kv.Value, received.Value)
	assert.Equal(t, kv.ExpireAt, received.ExpireAt)

	kv = &model.PluginKeyValue{
		PluginId: model.NewId(),
		Key:      model.NewId(),
		Value:    []byte(model.NewId()),
		ExpireAt: model.GetMillis() - 5000,
	}

	_, err = ss.Plugin().SaveOrUpdate(kv)
	require.Nil(t, err)

	defer func() {
		_ = ss.Plugin().Delete(kv.PluginId, kv.Key)
	}()

	_, err = ss.Plugin().Get(kv.PluginId, kv.Key)
	require.NotNil(t, err)
}

func testPluginDelete(t *testing.T, ss store.Store) {
	kv, err := ss.Plugin().SaveOrUpdate(&model.PluginKeyValue{
		PluginId: model.NewId(),
		Key:      model.NewId(),
		Value:    []byte(model.NewId()),
	})
	require.Nil(t, err)

	err = ss.Plugin().Delete(kv.PluginId, kv.Key)
	require.Nil(t, err)
}

func testPluginDeleteAll(t *testing.T, ss store.Store) {
	pluginId := model.NewId()

	kv, err := ss.Plugin().SaveOrUpdate(&model.PluginKeyValue{
		PluginId: pluginId,
		Key:      model.NewId(),
		Value:    []byte(model.NewId()),
	})
	require.Nil(t, err)

	kv2, err := ss.Plugin().SaveOrUpdate(&model.PluginKeyValue{
		PluginId: pluginId,
		Key:      model.NewId(),
		Value:    []byte(model.NewId()),
	})
	require.Nil(t, err)

	err = ss.Plugin().DeleteAllForPlugin(pluginId)
	require.Nil(t, err)

	_, err = ss.Plugin().Get(kv.PluginId, kv.Key)
	require.NotNil(t, err)

	_, err = ss.Plugin().Get(kv.PluginId, kv2.Key)
	require.NotNil(t, err)
}

func testPluginDeleteExpired(t *testing.T, ss store.Store) {
	pluginId := model.NewId()

	kv, err := ss.Plugin().SaveOrUpdate(&model.PluginKeyValue{
		PluginId: pluginId,
		Key:      model.NewId(),
		Value:    []byte(model.NewId()),
		ExpireAt: model.GetMillis() - 6000,
	})
	require.Nil(t, err)

	kv2, err := ss.Plugin().SaveOrUpdate(&model.PluginKeyValue{
		PluginId: pluginId,
		Key:      model.NewId(),
		Value:    []byte(model.NewId()),
		ExpireAt: 0,
	})
	require.Nil(t, err)

	err = ss.Plugin().DeleteAllExpired()
	require.Nil(t, err)

	_, err = ss.Plugin().Get(kv.PluginId, kv.Key)
	require.NotNil(t, err)

	received, err := ss.Plugin().Get(kv2.PluginId, kv2.Key)
	require.Nil(t, err)
	assert.Equal(t, kv2.PluginId, received.PluginId)
	assert.Equal(t, kv2.Key, received.Key)
	assert.Equal(t, kv2.Value, received.Value)
	assert.Equal(t, kv2.ExpireAt, received.ExpireAt)
}
