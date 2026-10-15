n=int(input()); a=list(map(int,input().split())); st=[]; ans=[0]*n
for i,x in enumerate(a):
    while st and a[st[-1]]<x:
        j=st.pop(); ans[j]=i-j
    st.append(i)
print(*ans)
