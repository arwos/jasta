/*
 *  Copyright (c) 2023 Mikhail Knyazhev <markus621@gmail.com>. All rights reserved.
 *  Use of this source code is governed by a BSD-3-Clause license that can be found in the LICENSE file.
 */

package utils

import "sort"

func Map2Slice(v map[string]struct{}) []string {
	data := make([]string, 0, len(v))
	for s := range v {
		data = append(data, s)
	}
	sort.Strings(data)
	return data
}
